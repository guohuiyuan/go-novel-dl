package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"
	"unicode/utf8"
)

type buildInfo struct {
	appID          string
	archs          []string
	ldflags        string
	minsdk         int
	targetsdk      int
	name           string
	pkgDir         string
	pkgPath        string
	iconPath       string
	tags           string
	target         string
	version        Semver
	key            string
	password       string
	notaryAppleID  string
	notaryPassword string
	notaryTeamID   string
	schemes        []string
	packageQueries []string
	slice          bool
}

type Semver struct {
	Major, Minor, Patch int
	VersionCode         uint32
}

func newBuildInfo(pkgPath string) (*buildInfo, error) {
	pkgMetadata, err := getPkgMetadata(pkgPath)
	if err != nil {
		return nil, err
	}
	appID := getAppID(pkgMetadata)
	appIcon := filepath.Join(pkgMetadata.Dir, "appicon.png")
	if *iconPath != "" {
		appIcon = *iconPath
	}
	appName := getPkgName(pkgMetadata)
	if *name != "" {
		appName = *name
	}
	ver, err := parseSemver(*version)
	if err != nil {
		return nil, err
	}
	sp := *signPass
	if sp == "" {
		sp = os.Getenv("GOGIO_SIGNPASS")
	}
	bi := &buildInfo{
		appID:          appID,
		archs:          getArchs(),
		ldflags:        getLdFlags(appID),
		minsdk:         *minsdk,
		targetsdk:      *targetsdk,
		name:           appName,
		pkgDir:         pkgMetadata.Dir,
		pkgPath:        pkgPath,
		iconPath:       appIcon,
		tags:           *extraTags,
		target:         *target,
		version:        ver,
		key:            *signKey,
		password:       sp,
		notaryAppleID:  *notaryID,
		notaryPassword: *notaryPass,
		notaryTeamID:   *notaryTeamID,
		schemes:        getCommaList(*schemes),
		packageQueries: getCommaList(*pkgQueries),
		slice:          *apkslice,
	}
	return bi, nil
}

// UppercaseName returns a string with its first rune in uppercase.
func UppercaseName(name string) string {
	ch, w := utf8.DecodeRuneInString(name)
	return string(unicode.ToUpper(ch)) + name[w:]
}

func (s Semver) String() string {
	return fmt.Sprintf("%d.%d.%d.%d", s.Major, s.Minor, s.Patch, s.VersionCode)
}

func (s Semver) StringCompact() string {
	// Used to meet CFBundleShortVersionString format.
	return fmt.Sprintf("%d.%d.%d", s.Major, s.Minor, s.Patch)
}

func parseSemver(v string) (Semver, error) {
	var sv Semver
	_, err := fmt.Sscanf(v, "%d.%d.%d.%d", &sv.Major, &sv.Minor, &sv.Patch, &sv.VersionCode)
	if err != nil || sv.String() != v {
		return Semver{}, fmt.Errorf("invalid semver: %q (must match major.minor.patch.versioncode)", v)
	}
	return sv, nil
}

func getArchs() []string {
	if *archNames != "" {
		return strings.Split(*archNames, ",")
	}
	switch *target {
	case "js":
		return []string{"wasm"}
	case "ios", "tvos":
		// Only 64-bit support.
		return []string{"arm64", "amd64"}
	case "android":
		return []string{"arm", "arm64", "386", "amd64"}
	case "windows":
		goarch := os.Getenv("GOARCH")
		if goarch == "" {
			goarch = runtime.GOARCH
		}
		return []string{goarch}
	case "macos":
		return []string{"arm64", "amd64"}
	default:
		// TODO: Add flag tests.
		panic("The target value has already been validated, this will never execute.")
	}
}

func getLdFlags(appID string) string {
	var ldflags []string
	if extra := *extraLdflags; extra != "" {
		ldflags = append(ldflags, strings.Split(extra, " ")...)
	}
	// Pass appID along, to be used for logging on platforms like Android.
	ldflags = append(ldflags, fmt.Sprintf("-X gioui.org/app.ID=%s", appID))
	// Support earlier Gio versions that had a separate app id recorded.
	// TODO: delete this in the future.
	ldflags = append(ldflags, fmt.Sprintf("-X gioui.org/app/internal/log.appID=%s", appID))
	// Pass along all remaining arguments to the app.
	if appArgs := flag.Args()[1:]; len(appArgs) > 0 {
		ldflags = append(ldflags, fmt.Sprintf("-X gioui.org/app.extraArgs=%s", strings.Join(appArgs, "|")))
	}
	if m := *linkMode; m != "" {
		ldflags = append(ldflags, "-linkmode="+m)
	}
	return strings.Join(ldflags, " ")
}

func getCommaList(s string) (list []string) {
	for _, v := range strings.Split(s, ",") {
		if v := strings.TrimSpace(v); v != "" {
			list = append(list, v)
		}
	}
	return list
}

type packageMetadata struct {
	PkgPath string
	Dir     string
}

func getPkgMetadata(pkgPath string) (*packageMetadata, error) {
	pkgImportPath, err := runCmd(exec.Command("go", "list", "-tags", *extraTags, "-f", "{{.ImportPath}}", pkgPath))
	if err != nil {
		return nil, err
	}
	pkgDir, err := runCmd(exec.Command("go", "list", "-tags", *extraTags, "-f", "{{.Dir}}", pkgPath))
	if err != nil {
		return nil, err
	}
	return &packageMetadata{
		PkgPath: pkgImportPath,
		Dir:     pkgDir,
	}, nil
}

func getAppID(pkgMetadata *packageMetadata) string {
	if *appID != "" {
		return *appID
	}
	elems := strings.Split(pkgMetadata.PkgPath, "/")
	domain := strings.Split(elems[0], ".")
	name := ""
	if len(elems) > 1 {
		name = "." + elems[len(elems)-1]
	}
	if len(elems) < 2 && len(domain) < 2 {
		name = "." + domain[0]
		domain[0] = "localhost"
	} else {
		for i := range len(domain) / 2 {
			opp := len(domain) - 1 - i
			domain[i], domain[opp] = domain[opp], domain[i]
		}
	}

	pkgDomain := strings.Join(domain, ".")
	appid := []rune(pkgDomain + name)

	// a Java-language-style package name may contain upper- and lower-case
	// letters and underscores with individual parts separated by '.'.
	// https://developer.android.com/guide/topics/manifest/manifest-element
	for i, c := range appid {
		if !('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' ||
			c == '_' || c == '.') {
			appid[i] = '_'
		}
	}
	return string(appid)
}

func getPkgName(pkgMetadata *packageMetadata) string {
	return path.Base(pkgMetadata.PkgPath)
}
