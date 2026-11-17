package org.gioui;

import android.os.Build;
import android.view.View;
import android.view.ViewGroup;
import android.webkit.WebSettings;
import android.webkit.WebView;

public final class NovelDLWebViewSupport {
    private static final int MAX_FOCUS_ATTEMPTS = 32;
    private static final long FOCUS_RETRY_DELAY_MS = 250;

    private NovelDLWebViewSupport() {}

    public static void focusWebViewWhenReady(View root) {
        focusWebViewWhenReady(root, MAX_FOCUS_ATTEMPTS);
    }

    private static void focusWebViewWhenReady(final View root, final int attemptsLeft) {
        View activityRoot = root.getRootView();
        WebView webView = findWebView(activityRoot);
        if (webView == null && activityRoot != root) {
            webView = findWebView(root);
        }
        if (webView != null) {
            WebSettings settings = webView.getSettings();
            settings.setJavaScriptEnabled(true);
            settings.setDomStorageEnabled(true);
            settings.setDatabaseEnabled(true);
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.LOLLIPOP) {
                settings.setMixedContentMode(WebSettings.MIXED_CONTENT_COMPATIBILITY_MODE);
            }
            webView.setFocusable(true);
            webView.setFocusableInTouchMode(true);
            if (webView.requestFocus()) {
                return;
            }
        }
        if (attemptsLeft <= 0) {
            return;
        }
        root.postDelayed(new Runnable() {
            @Override
            public void run() {
                focusWebViewWhenReady(root, attemptsLeft - 1);
            }
        }, FOCUS_RETRY_DELAY_MS);
    }

    private static WebView findWebView(View view) {
        if (view instanceof WebView) {
            return (WebView) view;
        }
        if (!(view instanceof ViewGroup)) {
            return null;
        }
        ViewGroup group = (ViewGroup) view;
        for (int index = 0; index < group.getChildCount(); index++) {
            WebView match = findWebView(group.getChildAt(index));
            if (match != null) {
                return match;
            }
        }
        return null;
    }
}
