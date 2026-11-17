// SPDX-License-Identifier: Unlicense OR MIT

package main

const mainUsage = `The gogio command builds and packages Gio (gioui.org) programs.

Usage:

	gogio -target <target> [flags] <package> [run arguments]

The gogio tool builds and packages Gio programs for platforms where additional
metadata or support files are required.

The package argument specifies an import path or a single Go source file to
package. Any run arguments are appended to os.Args at runtime.

Compiled Java class files from jar files in the package directory are
included in Android builds.

The mandatory -target flag selects the target platform: ios or android for the
mobile platforms, tvos for Apple's tvOS, js for WebAssembly/WebGL, macos for
MacOS and windows for Windows.

The -arch flag specifies a comma separated list of GOARCHs to include. The
default is all supported architectures.

The -o flag specifies an output file or directory, depending on the target.

The -buildmode flag selects the build mode. Two build modes are available, exe
and archive. Buildmode exe outputs an .ipa file for iOS or tvOS, an .apk file
for Android or a directory with the WebAssembly module and support files for
a browser.

The -ldflags and -tags flags pass extra linker flags and tags to the go tool.

As a special case for iOS or tvOS, specifying a path that ends with ".app"
will output an app directory suitable for a simulator.

The other buildmode is archive, which will output an .aar library for Android
or a .framework for iOS and tvOS.

The -icon flag specifies a path to a PNG image to use as app icon on iOS and Android.
If left unspecified, the appicon.png file from the main package is used
(if it exists).

The -appid flag specifies the package name for Android or the bundle id for
iOS and tvOS. A bundle id must be provisioned through Xcode before the gogio
tool can use it.

The -version flag specifies the semantic version for -buildmode exe. It must
be on the form major.minor.patch.versioncode where the version code is used for
the integer version number for Android, iOS and tvOS.

For Android builds the -minsdk flag specify the minimum SDK level. For example,
use -minsdk 22 to target Android 5.1 (Lollipop) and later.

For Windows builds the -minsdk flag specify the minimum OS version. For example,
use -mindk 10 to target Windows 10 and later, -minsdk 6 for Windows Vista and later.

For iOS builds the -minsdk flag specify the minimum iOS version. For example, 
use -mindk 15 to target iOS 15.0 and later.

For Android builds the -targetsdk flag specify the target SDK level. For example,
use -targetsdk 33 to target Android 13 (Tiramisu) and later.

The -work flag prints the path to the working directory and suppress
its deletion.

The -x flag will print all the external commands executed by the gogio tool.

The -signkey flag specifies the path of the keystore, used for signing Android apk/aab files
or specifies the name of key on Keychain to sign MacOS apps. On iOS and macOS it can be used 
to specify the path of a provisioning profile (.mobileprovision/.provisionprofile).

The -signpass flag specifies the password of the keystore, ignored if -signkey is not provided.
If -signpass is not sepecified it will be read from the environment variable GOGIO_SIGNPASS.

The -notaryid flag specifies the Apple ID to use for notarization of MacOS app.

The -notarypass flag specifies the password of the Apple ID, ignored if -notaryid is not 
provided. That must be an app-specific password, see https://support.apple.com/en-us/HT204397 
for details. If not provided, the password will be prompted.

The -notaryteamid flag specifies the team ID to use for notarization of MacOS app, ignored if
-notaryid is not provided.

The -schemes flag specifies a list of comma separated URI schemes that the program can 
handle. For example, use -schemes yourAppName to receive a app.URLEvent for URIs 
starting with yourAppName://. It is only supported on Android, iOS, macOS and Windows. 
On Windows, it will restrict the program to a single instance.

The -queries flag specifies a list of comma separated package names used to query other apps,
that is useful to launch other apps and verify their presence. For example, use -queries 
com.example.otherapp to query the app with that package name. It is only necessary on Android.
`
