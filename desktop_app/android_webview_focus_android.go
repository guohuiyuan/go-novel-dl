//go:build android

package main

import (
	"fmt"
	"log"

	"gioui.org/app"
	"git.wow.st/gmp/jni"
)

func (a *desktopApp) requestAndroidWebViewFocus(evt app.ViewEvent) {
	if a.androidWebViewFocusOnce {
		return
	}
	androidEvt, ok := evt.(app.AndroidViewEvent)
	if !ok || androidEvt.View == 0 {
		return
	}

	a.androidWebViewFocusOnce = true
	view := jni.Object(androidEvt.View)
	go a.window.Run(func() {
		if err := focusAndroidWebView(view); err != nil {
			log.Printf("request Android WebView focus: %v", err)
		}
	})
}

func focusAndroidWebView(view jni.Object) error {
	return jni.Do(jni.JVMFor(app.JavaVM()), func(env jni.Env) error {
		viewClass := jni.GetObjectClass(env, view)
		context, err := jni.CallObjectMethod(
			env,
			view,
			jni.GetMethodID(env, viewClass, "getContext", "()Landroid/content/Context;"),
		)
		if err != nil {
			return err
		}
		loader := jni.ClassLoaderFor(env, context)
		if loader == 0 {
			return fmt.Errorf("Android class loader is unavailable")
		}
		class, err := jni.LoadClass(env, loader, "org.gioui.NovelDLWebViewSupport")
		if err != nil {
			return err
		}
		method := jni.GetStaticMethodID(env, class, "focusWebViewWhenReady", "(Landroid/view/View;)V")
		return jni.CallStaticVoidMethod(env, class, method, jni.Value(view))
	})
}
