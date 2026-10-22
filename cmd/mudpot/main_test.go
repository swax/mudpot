package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIInitAndValidate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "custom")
	var out bytes.Buffer
	if err := run(context.Background(), []string{"init", "-dir", dir}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{"validate", "-adventure", dir}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Valid: Grey Sector") {
		t.Fatal(out.String())
	}
	if err := run(context.Background(), []string{"init", "-dir", dir}, &out); err == nil {
		t.Fatal("overwrite allowed")
	}
	if err := run(context.Background(), []string{"validate", "unexpected"}, &out); err == nil {
		t.Fatal("unexpected argument accepted")
	}
}
func TestNoListenersAndInvalidLimits(t *testing.T) {
	for _, args := range [][]string{{"serve", "-tcp=", "-ssh=", "-tls="}, {"serve", "-max-connections=0"}, {"serve", "-tcp=bad-address"}} {
		if err := run(context.Background(), args, &bytes.Buffer{}); err == nil {
			t.Fatal("invalid config accepted", args)
		}
	}
}
