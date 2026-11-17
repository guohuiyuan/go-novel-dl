//go:build !android

package main

import "gioui.org/app"

func (a *desktopApp) requestAndroidWebViewFocus(app.ViewEvent) {}
