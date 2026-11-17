package main

import "testing"

func TestAppID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in, out string
	}{
		{"example", "localhost.example"},
		{"example.com", "com.example"},
		{"www.example.com", "com.example.www"},
		{"examplecom/app", "examplecom.app"},
		{"example.com/app", "com.example.app"},
		{"www.example.com/app", "com.example.www.app"},
		{"www.en.example.com/app", "com.example.en.www.app"},
		{"example.com/dir/app", "com.example.app"},
		{"example.com/dir.ext/app", "com.example.app"},
		{"example.com/dir/app.ext", "com.example.app.ext"},
		{"example-com.net/dir/app", "net.example_com.app"},
	}

	for i, test := range tests {
		got := getAppID(&packageMetadata{PkgPath: test.in})
		if exp := test.out; got != exp {
			t.Errorf("(%d): expected '%s', got '%s'", i, exp, got)
		}
	}
}

func TestVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		version string
		valid   bool
	}{
		{"v1", false},
		{"v10.21.333.12", false},
		{"1.2.3", false},
		{"1.2.3.4", true},
	}

	for i, test := range tests {
		ver, err := parseSemver(test.version)
		if err != nil {
			if test.valid {
				t.Errorf("(%d): %q failed to parse: %v", i, test.version, err)
			}
			continue
		} else if !test.valid {
			t.Errorf("(%d): %q was unexpectedly accepted", i, test.version)
		}
		if got := ver.String(); got != test.version {
			t.Errorf("(%d): %q parsed to %q", i, test.version, got)
		}
	}
}
