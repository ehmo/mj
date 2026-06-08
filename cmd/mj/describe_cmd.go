package main

import (
	"context"
	"flag"
	"fmt"
)

// cmdDescribe describes an image (URL or local file) → suggested prompts.
func cmdDescribe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("describe", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "")
	pos := parseArgs(fs, args)
	if len(pos) < 1 {
		return fmt.Errorf("usage: mj describe IMAGE   (URL or local file path)")
	}
	c, _, err := openClient(ctx, false)
	if err != nil {
		return err
	}
	defer c.Close()
	prompts, err := c.DescribeImage(ctx, pos[0])
	if err != nil {
		return err
	}
	if *jsonOut {
		printJSON(map[string]any{"descriptions": prompts})
		return nil
	}
	for i, p := range prompts {
		fmt.Printf("%d. %s\n", i+1, p)
	}
	return nil
}
