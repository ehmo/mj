package main

import (
	"context"
	"flag"
	"fmt"
	"strings"
)

// cmdAPI is a raw escape hatch onto any midjourney.com/api endpoint, so every
// capability (folders, personalized-profiles, model-ratings, following, and
// anything else) is reachable even before it gets a typed command.
//
//	mj api GET  /api/folders
//	mj api POST /api/user-flags --data '{"flag":"x"}'
func cmdAPI(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("api", flag.ExitOnError)
	data := fs.String("data", "", "JSON request body (implies POST)")
	pos := parseArgs(fs, args)

	var method, path string
	switch {
	case len(pos) >= 2:
		method, path = strings.ToUpper(pos[0]), pos[1]
	case len(pos) == 1:
		path = pos[0]
		if *data != "" {
			method = "POST"
		} else {
			method = "GET"
		}
	default:
		return fmt.Errorf("usage: mj api <METHOD> <path> [--data JSON]")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	c, _, err := openClient(ctx, false)
	if err != nil {
		return err
	}
	defer c.Close()

	var b []byte
	if method == "GET" {
		b, err = c.Get(ctx, path)
	} else {
		var body []byte
		if *data != "" {
			body = []byte(*data)
		}
		b, err = c.PostJSON(ctx, path, body)
	}
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}
