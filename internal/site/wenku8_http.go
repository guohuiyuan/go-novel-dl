package site

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"time"

	utls "github.com/refraction-networking/utls"
)

// newWenku8HTTPClient 返回使用 Chrome TLS 指纹的 HTTP 客户端，并绕过系统代理直连。
// 原因：
//  1. wenku8 套了 Cloudflare，Go 默认 crypto/tls 的 ClientHello 指纹会被识别拦截（403）；
//     uTLS 模拟 Chrome 指纹可正常访问。
//  2. 本机代理（如 Clash）会干扰 uTLS 握手（EOF），且 wenku8 是国内站点通常直连可达，
//     cf_clearance 也绑定浏览器所在 IP，因此直接连接。
func newWenku8HTTPClient(timeout time.Duration, jar http.CookieJar) *http.Client {
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return wenku8UTLSDial(ctx, dialer, network, addr)
		},
		ForceAttemptHTTP2: false,
		TLSNextProto:      make(map[string]func(string, *tls.Conn) http.RoundTripper),
		MaxIdleConns:      10,
		IdleConnTimeout:   60 * time.Second,
	}
	return &http.Client{Timeout: timeout, Jar: jar, Transport: transport}
}

func wenku8UTLSDial(ctx context.Context, dialer *net.Dialer, network, addr string) (net.Conn, error) {
	raw, err := dialer.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		raw.Close()
		return nil, err
	}
	uconn := utls.UClient(raw, &utls.Config{ServerName: host}, utls.HelloGolang)
	// Chrome 指纹规避 Cloudflare 拦截；同时把 ALPN 限制为 http/1.1
	// （Go 的 transport 未启用 h2，若协商出 h2，服务端会发 SETTINGS 帧导致 HTTP/1.x 解析错乱）。
	if spec, err := utls.UTLSIdToSpec(utls.HelloChrome_120); err == nil {
		for i := range spec.Extensions {
			if alpn, ok := spec.Extensions[i].(*utls.ALPNExtension); ok {
				alpn.AlpnProtocols = []string{"http/1.1"}
			}
		}
		_ = uconn.ApplyPreset(&spec)
	}
	if err := uconn.HandshakeContext(ctx); err != nil {
		raw.Close()
		return nil, err
	}
	return uconn, nil
}
