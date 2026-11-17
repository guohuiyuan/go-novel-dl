package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
)

func buildMac(tmpDir string, bi *buildInfo) error {
	builder := &macBuilder{TempDir: tmpDir}
	builder.DestDir = *destPath
	if builder.DestDir == "" {
		builder.DestDir = bi.pkgPath
	}

	name := bi.name
	if *destPath != "" {
		if filepath.Ext(*destPath) != ".app" {
			return fmt.Errorf("invalid output name %q, it must end with `.app`", *destPath)
		}
		name = filepath.Base(*destPath)
	}
	name = strings.TrimSuffix(name, ".app")

	if bi.appID == "" {
		return errors.New("app id is empty; use -appid to set it")
	}

	if err := builder.setIcon(bi.iconPath); err != nil {
		return err
	}

	builder.setInfo(bi, name)

	for _, arch := range bi.archs {
		tmpDest := filepath.Join(builder.TempDir, filepath.Base(builder.DestDir))
		finalDest := builder.DestDir
		if len(bi.archs) > 1 {
			tmpDest = filepath.Join(builder.TempDir, name+"_"+arch+".app")
			finalDest = filepath.Join(builder.DestDir, name+"_"+arch+".app")
		}

		if err := builder.buildProgram(bi, tmpDest, name, arch); err != nil {
			return err
		}

		if bi.key != "" {
			if err := builder.signProgram(bi, tmpDest, name, arch); err != nil {
				return err
			}
		}

		if err := dittozip(tmpDest, tmpDest+".zip"); err != nil {
			return err
		}

		if bi.notaryAppleID != "" {
			if err := builder.notarize(bi, tmpDest+".zip"); err != nil {
				return err
			}
		}

		if err := dittounzip(tmpDest+".zip", finalDest); err != nil {
			return err
		}
	}

	return nil
}

type macBuilder struct {
	TempDir string
	DestDir string

	Icons        []byte
	Manifest     []byte
	Entitlements []byte
}

func (b *macBuilder) setIcon(path string) (err error) {
	if _, err := os.Stat(path); err != nil {
		return nil
	}

	out := filepath.Join(b.TempDir, "iconset.iconset")
	if err := os.MkdirAll(out, 0o777); err != nil {
		return err
	}

	err = buildIcons(out, path, []iconVariant{
		{path: "icon_512x512@2x.png", size: 1024},
		{path: "icon_512x512.png", size: 512},
		{path: "icon_256x256@2x.png", size: 512},
		{path: "icon_256x256.png", size: 256},
		{path: "icon_128x128@2x.png", size: 256},
		{path: "icon_128x128.png", size: 128},
		{path: "icon_64x64@2x.png", size: 128},
		{path: "icon_64x64.png", size: 64},
		{path: "icon_32x32@2x.png", size: 64},
		{path: "icon_32x32.png", size: 32},
		{path: "icon_16x16@2x.png", size: 32},
		{path: "icon_16x16.png", size: 16},
	})
	if err != nil {
		return err
	}

	cmd := exec.Command("iconutil",
		"-c", "icns", out,
		"-o", filepath.Join(b.TempDir, "icon.icns"))
	if _, err := runCmd(cmd); err != nil {
		return err
	}

	b.Icons, err = os.ReadFile(filepath.Join(b.TempDir, "icon.icns"))
	return err
}

func (b *macBuilder) setInfo(buildInfo *buildInfo, name string) {

	manifestSrc := struct {
		Name    string
		Bundle  string
		Version Semver
		Schemes []string
	}{
		Name:    name,
		Bundle:  buildInfo.appID,
		Version: buildInfo.version,
		Schemes: buildInfo.schemes,
	}

	t, err := template.New("manifest").Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleExecutable</key>
	<string>{{.Name}}</string>
	<key>CFBundleIconFile</key>
	<string>icon.icns</string>
	<key>CFBundleIdentifier</key>
	<string>{{.Bundle}}</string>
	<key>NSHighResolutionCapable</key>
	<true/>
	<key>CFBundlePackageType</key>
	<string>BNDL</string>
    {{if .Schemes}}
	<key>CFBundleURLTypes</key>
	<array>
	  {{range .Schemes}}
	  <dict>
		<key>CFBundleURLSchemes</key>
		<array>
		  <string>{{.}}</string>
		</array>
	  </dict>
	  {{end}}
	</array>
    {{end}}
</dict>
</plist>`)
	if err != nil {
		panic(err)
	}

	var manifest bytes.Buffer
	if err := t.Execute(&manifest, manifestSrc); err != nil {
		panic(err)
	}
	b.Manifest = manifest.Bytes()

	b.Entitlements = []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
<key>com.apple.security.cs.allow-unsigned-executable-memory</key>
<true/>
<key>com.apple.security.cs.allow-jit</key>
<true/>
</dict>
</plist>`)
}

func (b *macBuilder) buildProgram(buildInfo *buildInfo, binDest string, name string, arch string) error {
	for _, path := range []string{"/Contents/MacOS", "/Contents/Resources"} {
		if err := os.MkdirAll(filepath.Join(binDest, path), 0o755); err != nil {
			return err
		}
	}

	if len(b.Icons) > 0 {
		if err := os.WriteFile(filepath.Join(binDest, "/Contents/Resources/icon.icns"), b.Icons, 0o755); err != nil {
			return err
		}
	}

	if err := os.WriteFile(filepath.Join(binDest, "/Contents/Info.plist"), b.Manifest, 0o755); err != nil {
		return err
	}

	cmd := exec.Command(
		"go",
		"build",
		"-ldflags="+buildInfo.ldflags,
		"-tags="+buildInfo.tags,
		"-o", filepath.Join(binDest, "/Contents/MacOS/"+name),
		buildInfo.pkgPath,
	)
	cmd.Env = append(
		os.Environ(),
		"GOOS=darwin",
		"GOARCH="+arch,
		"CGO_ENABLED=1", // Required to cross-compile between AMD/ARM
	)
	_, err := runCmd(cmd)
	return err
}

func (b *macBuilder) signProgram(buildInfo *buildInfo, binDest string, name string, arch string) error {
	options := filepath.Join(b.TempDir, "ent.ent")
	if err := os.WriteFile(options, b.Entitlements, 0o777); err != nil {
		return err
	}

	xattr := exec.Command("xattr", "-rc", binDest)
	if _, err := runCmd(xattr); err != nil {
		return err
	}

	// If the key is a provisioning profile use the same signing process as iOS
	if filepath.Ext(buildInfo.key) == ".provisionprofile" {
		embedded := filepath.Join(binDest, "Contents", "embedded.provisionprofile")
		return signApple(buildInfo.appID, b.TempDir, embedded, binDest, []string{buildInfo.key})
	}

	cmd := exec.Command(
		"codesign",
		"--deep",
		"--force",
		"--options", "runtime",
		"--entitlements", options,
		"--sign", buildInfo.key,
		binDest,
	)
	_, err := runCmd(cmd)
	return err
}

func (b *macBuilder) notarize(buildInfo *buildInfo, binDest string) error {
	cmd := exec.Command(
		"xcrun",
		"notarytool",
		"submit",
		binDest,
		"--apple-id", buildInfo.notaryAppleID,
		"--team-id", buildInfo.notaryTeamID,
		"--wait",
	)

	if buildInfo.notaryPassword != "" {
		cmd.Args = append(cmd.Args, "--password", buildInfo.notaryPassword)
	}

	_, err := runCmd(cmd)
	return err
}

func dittozip(input, output string) error {
	cmd := exec.Command("ditto", "-c", "-k", "-X", "--rsrc", input, output)

	_, err := runCmd(cmd)
	return err
}

func dittounzip(input, output string) error {
	cmd := exec.Command("ditto", "-x", "-k", "-X", "--rsrc", input, output)

	_, err := runCmd(cmd)
	return err
}
