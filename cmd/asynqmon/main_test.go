package main

import (
	"crypto/tls"
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/hibiken/asynq"
)

func TestParseFlags(t *testing.T) {
	tests := []struct {
		args []string
		want *Config
	}{
		{
			args: []string{"--redis-addr", "localhost:6380", "--redis-db", "3"},
			want: &Config{
				RedisAddr: "localhost:6380",
				RedisDB:   3,

				// Default values
				Port:                  8080,
				RedisPassword:         "",
				RedisTLS:              "",
				RedisURL:              "",
				RedisInsecureTLS:      false,
				RedisClusterNodes:     "",
				MaxPayloadLength:      200,
				MaxResultLength:       200,
				EnableMetricsExporter: false,
				PrometheusServerAddr:  "",
				ReadOnly:              false,
				AuthCookieSecure:      true,

				Args: []string{},
			},
		},
	}

	for _, tc := range tests {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			cfg, output, err := parseFlags("asynqmon", tc.args)
			if err != nil {
				t.Errorf("parseFlags returned error: %v", err)
			}
			if output != "" {
				t.Errorf("parseFlag returned output=%q, want empty", output)
			}
			if diff := cmp.Diff(tc.want, cfg); diff != "" {
				t.Errorf("parseFlag returned Config %v, want %v; (-want,+got)\n%s", cfg, tc.want, diff)
			}
		})
	}

}

func TestMakeRedisConnOpt(t *testing.T) {
	var tests = []struct {
		desc string
		cfg  *Config
		want asynq.RedisConnOpt
	}{
		{
			desc: "With address, db number and password",
			cfg: &Config{
				RedisAddr:     "localhost:6380",
				RedisDB:       1,
				RedisPassword: "foo",
			},
			want: asynq.RedisClientOpt{
				Addr:     "localhost:6380",
				DB:       1,
				Password: "foo",
			},
		},
		{
			desc: "With TLS server name",
			cfg: &Config{
				RedisAddr: "localhost:6379",
				RedisTLS:  "foobar",
			},
			want: asynq.RedisClientOpt{
				Addr:      "localhost:6379",
				TLSConfig: &tls.Config{ServerName: "foobar"},
			},
		},
		{
			desc: "With redis URL",
			cfg: &Config{
				RedisURL: "redis://:bar@localhost:6381/2",
			},
			want: asynq.RedisClientOpt{
				Addr:     "localhost:6381",
				DB:       2,
				Password: "bar",
			},
		},
		{
			desc: "With redis-sentinel URL",
			cfg: &Config{
				RedisURL: "redis-sentinel://:secretpassword@localhost:5000,localhost:5001,localhost:5002?master=mymaster",
			},
			want: asynq.RedisFailoverClientOpt{
				MasterName: "mymaster",
				SentinelAddrs: []string{
					"localhost:5000", "localhost:5001", "localhost:5002"},
				Password: "secretpassword", // FIXME: Shouldn't this be SentinelPassword instead?
			},
		},
		{
			desc: "With cluster nodes",
			cfg: &Config{
				RedisClusterNodes: "localhost:5000,localhost:5001,localhost:5002,localhost:5003,localhost:5004,localhost:5005",
			},
			want: asynq.RedisClusterClientOpt{
				Addrs: []string{
					"localhost:5000", "localhost:5001", "localhost:5002", "localhost:5003", "localhost:5004", "localhost:5005"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := makeRedisConnOpt(tc.cfg)
			if err != nil {
				t.Fatalf("makeRedisConnOpt returned error: %v", err)
			}

			if diff := cmp.Diff(tc.want, got, cmpopts.IgnoreUnexported(tls.Config{})); diff != "" {
				t.Errorf("diff found: want=%v, got=%v; (-want,+got)\n%s",
					tc.want, got, diff)
			}
		})
	}
}

func TestAuthFlags(t *testing.T) {
	setEnv := func(key, value string) {
		previous, exists := os.LookupEnv(key)
		if err := os.Setenv(key, value); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if exists {
				_ = os.Setenv(key, previous)
			} else {
				_ = os.Unsetenv(key)
			}
		})
	}
	setEnv("BASIC_AUTH_USER", "operator")
	setEnv("BASIC_AUTH_PASSWORD", "test-password")
	setEnv("AUTH_COOKIE_SECURE", "false")
	cfg, _, err := parseFlags("asynqmon", nil)
	_, help, _ := parseFlags("asynqmon", []string{"--help"})
	if strings.Contains(help, "test-password") {
		t.Fatal("help exposes password from environment")
	}
	if err != nil || cfg.AuthUsername != "operator" || cfg.AuthPassword != "test-password" || cfg.AuthCookieSecure {
		t.Fatal("auth env not parsed")
	}
	cfg, _, err = parseFlags("asynqmon", []string{"--auth-user=admin", "--auth-password=override", "--auth-cookie-secure=true"})
	if err != nil || cfg.AuthUsername != "admin" || cfg.AuthPassword != "override" || !cfg.AuthCookieSecure {
		t.Fatal("flags must override env")
	}
}
