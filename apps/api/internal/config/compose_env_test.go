package config

import (
	"bufio"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// composeFile is the production stack, relative to this package.
var composeFile = filepath.Join("..", "..", "..", "..", "compose.prod.yml")

// notForwarded are the variables compose.prod.yml deliberately leaves out of
// the api environment, each with the reason it cannot be set from .env.
var notForwarded = map[string]string{
	"TC_PORT": "the container port is fixed by the published port and the web service's upstream",
}

// divergentDefaults are the forwarded variables whose compose default
// deliberately differs from the one config.go applies when unset.
var divergentDefaults = map[string]string{
	"TC_TRUSTED_PROXIES": "every request arrives through the web service on the compose network",
	"TC_S3_ENDPOINT":     "the object store is the storage service, not localhost",
	"TC_METRICS_ADDR":    "a container cannot bind the host's loopback",
}

// apiEnvironment returns the api service's environment entries, keyed by
// variable name, from the compose file.
func apiEnvironment(t *testing.T) map[string]string {
	t.Helper()
	f, err := os.Open(composeFile)
	if err != nil {
		t.Fatalf("open %s: %v", composeFile, err)
	}
	defer f.Close()

	entry := regexp.MustCompile(`^      (TC_[A-Z0-9_]+):\s*(.*)$`)
	out := map[string]string{}
	inAPI, inEnv := false, false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") && strings.HasSuffix(line, ":"):
			inAPI, inEnv = line == "  api:", false
		case inAPI && strings.HasPrefix(line, "    ") && !strings.HasPrefix(line, "     ") && strings.HasSuffix(line, ":"):
			inEnv = line == "    environment:"
		case inAPI && inEnv:
			if m := entry.FindStringSubmatch(line); m != nil {
				out[m[1]] = m[2]
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatalf("no api environment found in %s", composeFile)
	}
	return out
}

// configVariables returns every variable Config reads, with its default.
func configVariables() map[string]string {
	out := map[string]string{}
	rt := reflect.TypeOf(Config{})
	for i := range rt.NumField() {
		if name := rt.Field(i).Tag.Get("env"); name != "" {
			out[name] = rt.Field(i).Tag.Get("envDefault")
		}
	}
	return out
}

// The api environment is the only route from .env to the process, so a
// variable missing from it is ignored however the operator sets it.
func TestComposeForwardsEveryConfigVariable(t *testing.T) {
	forwarded := apiEnvironment(t)
	read := configVariables()

	for name := range read {
		_, ok := forwarded[name]
		if _, exempt := notForwarded[name]; exempt {
			if ok {
				t.Errorf("%s is listed as not forwarded but compose.prod.yml forwards it", name)
			}
			continue
		}
		if !ok {
			t.Errorf("%s is read by the API but compose.prod.yml does not forward it", name)
		}
	}
	for name := range forwarded {
		if _, ok := read[name]; !ok {
			t.Errorf("compose.prod.yml forwards %s, which the API does not read", name)
		}
	}
}

// Unset in .env, a forwarded variable has to mean what it means to the API
// when unset, or the compose deployment quietly runs a different default.
func TestComposeDefaultsMatchConfigDefaults(t *testing.T) {
	ref := regexp.MustCompile(`^\$\{([A-Z0-9_]+)(:?-)(.*)\}$`)
	read := configVariables()
	for name, value := range apiEnvironment(t) {
		m := ref.FindStringSubmatch(strings.Trim(value, `"`))
		if m == nil || m[1] != name {
			continue
		}
		if _, ok := divergentDefaults[name]; ok {
			continue
		}
		if want := read[name]; m[3] != want {
			t.Errorf("%s defaults to %q in compose.prod.yml but %q in config.go", name, m[3], want)
		}
	}
}

// Every client reaches the API through the web service, so the shipped
// default has to trust that hop or all of them share one rate-limit bucket.
func TestComposeTrustsTheWebServiceHop(t *testing.T) {
	value := apiEnvironment(t)["TC_TRUSTED_PROXIES"]
	m := regexp.MustCompile(`^\$\{TC_TRUSTED_PROXIES:-(.+)\}$`).FindStringSubmatch(value)
	if m == nil {
		t.Fatalf("TC_TRUSTED_PROXIES has no non-empty default in compose.prod.yml: %s", value)
	}
	// An address from the first pool Docker allocates compose networks from.
	hop := netip.MustParseAddr("172.18.0.3")
	c := &Config{TrustedProxies: m[1]}
	for _, p := range c.TrustedProxyList() {
		if p.Contains(hop) {
			return
		}
	}
	t.Fatalf("default %q does not cover a compose network address", m[1])
}
