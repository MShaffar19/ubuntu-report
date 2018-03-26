package main

import (
	"bufio"
	"bytes"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/ubuntu/ubuntu-report/internal/helper"
)

const (
	expectedReportItem = `"Version":`
	optOutJSON         = `{"OptOut": true}`
)

func TestShow(t *testing.T) {
	helper.SkipIfShort(t)
	a := helper.Asserter{T: t}
	stdout, restoreStdout := helper.CaptureStdout(t)
	defer restoreStdout()

	cmd := generateRootCmd()
	cmd.SetArgs([]string{"show"})

	var c *cobra.Command
	cmdErrs := helper.RunFunctionWithTimeout(t, func() error {
		var err error
		c, err = cmd.ExecuteC()
		restoreStdout() // close stdout to release ReadAll()
		return err
	})

	if err := <-cmdErrs; err != nil {
		t.Fatal("got an error when expecting none:", err)
	}
	a.Equal(c.Name(), "show")
	got, err := ioutil.ReadAll(stdout)
	if err != nil {
		t.Error("couldn't read from stdout", err)
	}
	if !strings.Contains(string(got), expectedReportItem) {
		t.Errorf("Expected %s to be in output, but got: %s", expectedReportItem, string(got))
	}
}

// Test Verbosity level with Show
func TestVerbosity(t *testing.T) {
	helper.SkipIfShort(t)

	testCases := []struct {
		verbosity string
	}{
		{""},
		{"-v"},
		{"-vv"},
	}
	for _, tc := range testCases {
		tc := tc // capture range variable for parallel execution
		t.Run("verbosity level "+tc.verbosity, func(t *testing.T) {
			a := helper.Asserter{T: t}
			out, restoreLogs := helper.CaptureLogs(t)
			defer restoreLogs()

			cmd := generateRootCmd()
			args := []string{"show"}
			if tc.verbosity != "" {
				args = append(args, tc.verbosity)
			}
			cmd.SetArgs(args)

			cmdErrs := helper.RunFunctionWithTimeout(t, func() error {
				var err error
				_, err = cmd.ExecuteC()
				restoreLogs() // send EOF to log to release io.Copy()
				return err

			})

			var got bytes.Buffer
			io.Copy(&got, out)

			if err := <-cmdErrs; err != nil {
				t.Fatal("got an error when expecting none:", err)
			}

			switch tc.verbosity {
			case "":
				a.Equal(got.String(), "")
			case "-v":
				// empty logs, apart info on installer or upgrade telemetry (file can be missing)
				// and other GPU, screen and autologin that you won't have in Travis CI.
				scanner := bufio.NewScanner(bytes.NewReader(got.Bytes()))
				for scanner.Scan() {
					l := scanner.Text()
					if strings.Contains(l, "level=info") {
						allowedLog := false
						for _, msg := range []string{"/telemetry", "GPU info", "Screen info", "autologin information"} {
							if strings.Contains(l, msg) {
								allowedLog = true
							}
						}
						if allowedLog {
							continue
						}
						t.Errorf("Expected no log output with -v apart from missing telemetry, GPU, Screen and autologin information, but got: %s", l)
					}
				}
			case "-vv":
				if !strings.Contains(got.String(), "level=debug") {
					t.Errorf("Expected some debug log to be printed, but got: %s", got.String())
				}
			}
		})
	}
}

func TestSend(t *testing.T) {
	helper.SkipIfShort(t)

	testCases := []struct {
		name   string
		answer string

		shouldHitServer bool
		wantErr         bool
	}{
		{"regular report auto", "yes", true, false},
		{"regular report opt-out", "no", true, false},
	}
	for _, tc := range testCases {
		tc := tc // capture range variable for parallel execution
		t.Run(tc.name, func(t *testing.T) {
			a := helper.Asserter{T: t}

			out, tearDown := helper.TempDir(t)
			defer tearDown()
			defer helper.ChangeEnv("XDG_CACHE_HOME", out)()
			out = filepath.Join(out, "ubuntu-report")
			// we don't really care where we hit for this API integration test, internal ones test it
			// and we don't really control /etc/os-release version and id.
			// Same for report file
			serverHit := false
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				serverHit = true
			}))
			defer ts.Close()

			cmd := generateRootCmd()
			args := []string{"send", tc.answer, "--url", ts.URL}
			cmd.SetArgs(args)

			cmdErrs := helper.RunFunctionWithTimeout(t, func() error {
				var err error
				_, err = cmd.ExecuteC()
				return err
			})

			if err := <-cmdErrs; err != nil {
				t.Fatal("got an error when expecting none:", err)
			}

			a.Equal(serverHit, tc.shouldHitServer)
			p := filepath.Join(out, helper.FindInDirectory(t, "", out))
			data, err := ioutil.ReadFile(p)
			if err != nil {
				t.Fatalf("couldn't open report file %s", out)
			}
			d := string(data)

			switch tc.answer {
			case "yes":
				if !strings.Contains(d, expectedReportItem) {
					t.Errorf("we expected to find %s in report file, got: %s", expectedReportItem, d)
				}
			case "no":
				if !strings.Contains(d, optOutJSON) {
					t.Errorf("we expected to find %s in report file, got: %s", optOutJSON, d)
				}
			}
		})
	}
}
