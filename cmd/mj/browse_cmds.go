package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ehmo/mj/internal/mjapi"
	"github.com/ehmo/mj/internal/mjclient"
)

func joinPos(pos []string) string { return strings.TrimSpace(strings.Join(pos, " ")) }

func cmdSearch(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	page := fs.Int("page", 1, "1-based page")
	limit := fs.Int("limit", 20, "max results to show")
	download := fs.String("download", "", "download result images to dir")
	format := fs.String("format", "png", "png|webp")
	size := fs.Int("size", 0, "thumbnail edge px; 0 = full-res")
	jsonOut := fs.Bool("json", false, "")
	pos := parseArgs(fs, args)
	query := joinPos(pos)
	if query == "" {
		return fmt.Errorf("usage: mj search \"QUERY\" [--page N] [--limit N] [--download DIR] [--format webp] [--size N]")
	}
	opts, err := assetOpts(*format, *size)
	if err != nil {
		return err
	}
	c, _, err := openClient(ctx, false)
	if err != nil {
		return err
	}
	defer c.Close()
	items, err := c.Search(ctx, query, *page)
	if err != nil {
		return err
	}
	return emitExplore(ctx, c, items, *limit, *download, opts, *jsonOut)
}

func cmdExplore(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("explore", flag.ExitOnError)
	feed := fs.String("feed", "top", "top|likes|random")
	page := fs.Int("page", 0, "0-based page")
	limit := fs.Int("limit", 20, "max results to show")
	download := fs.String("download", "", "download result images to dir")
	format := fs.String("format", "png", "png|webp")
	size := fs.Int("size", 0, "thumbnail edge px; 0 = full-res")
	jsonOut := fs.Bool("json", false, "")
	parseArgs(fs, args)
	opts, err := assetOpts(*format, *size)
	if err != nil {
		return err
	}
	c, _, err := openClient(ctx, false)
	if err != nil {
		return err
	}
	defer c.Close()
	items, err := c.Explore(ctx, mjclient.ExploreFeed(*feed), *page)
	if err != nil {
		return err
	}
	return emitExplore(ctx, c, items, *limit, *download, opts, *jsonOut)
}

func emitExplore(ctx context.Context, c *mjclient.Client, items []mjapi.ExploreItem, limit int, download string, opts mjapi.AssetOpts, jsonOut bool) error {
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	if jsonOut {
		printJSON(items)
	} else {
		for _, it := range items {
			tags := ""
			if it.Video {
				tags += " [video]"
			}
			if it.LikedByUser {
				tags += " ♥"
			}
			cmd := it.Command
			if cmd == "" {
				cmd = it.Prompt
			}
			fmt.Printf("%s  @%s%s  %s\n    %s\n", it.ID, orDash(it.Username), tags, it.ImageURL, truncate(cmd, 100))
		}
		fmt.Printf("(%d results)\n", len(items))
	}
	if download != "" {
		var saved []string
		for _, it := range items {
			files, err := c.DownloadOpts(ctx, exploreJob(it), download, mjclient.SelCells, opts)
			if err != nil {
				fmt.Fprintln(os.Stderr, "download", it.ID, "failed:", err)
				continue
			}
			saved = append(saved, files...)
		}
		fmt.Printf("downloaded %d files to %s\n", len(saved), download)
	}
	return nil
}

// exploreJob builds the minimal Job needed to derive an explore result's CDN
// assets: its real cell count and (for videos) a job_type that IsVideo detects.
func exploreJob(it mjapi.ExploreItem) mjapi.Job {
	batch := it.BatchSize
	if batch <= 0 {
		batch = 1
	}
	jt := it.JobType
	if it.Video {
		jt = "video" // force IsVideo() regardless of the (possibly v*-prefixed) job_type
	}
	return mjapi.Job{ID: it.ID, BatchSize: batch, JobType: jt}
}

// cmdLikes: mj likes [--page N] [--download DIR] [--format/--size]
func cmdLikes(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("likes", flag.ExitOnError)
	page := fs.Int("page", 1, "1-based page")
	limit := fs.Int("limit", 35, "max results to show")
	download := fs.String("download", "", "download result images to dir")
	format := fs.String("format", "png", "png|webp")
	size := fs.Int("size", 0, "thumbnail edge px; 0 = full-res")
	jsonOut := fs.Bool("json", false, "")
	parseArgs(fs, args)
	opts, err := assetOpts(*format, *size)
	if err != nil {
		return err
	}
	c, _, err := openClient(ctx, false)
	if err != nil {
		return err
	}
	defer c.Close()
	items, err := c.Likes(ctx, *page)
	if err != nil {
		return err
	}
	return emitExplore(ctx, c, items, *limit, *download, opts, *jsonOut)
}

// cmdStyles: mj styles [--page N] [--limit N] — browse community style references.
func cmdStyles(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("styles", flag.ExitOnError)
	page := fs.Int("page", 0, "0-based page")
	limit := fs.Int("limit", 30, "max results to show")
	jsonOut := fs.Bool("json", false, "")
	parseArgs(fs, args)
	c, _, err := openClient(ctx, false)
	if err != nil {
		return err
	}
	defer c.Close()
	items, err := c.Styles(ctx, *page)
	if err != nil {
		return err
	}
	if *limit > 0 && len(items) > *limit {
		items = items[:*limit]
	}
	if *jsonOut {
		printJSON(items)
		return nil
	}
	for _, it := range items {
		fmt.Printf("--sref %-14s  @%s\n", orDash(it.Sref), orDash(it.DisplayName))
	}
	fmt.Printf("(%d styles — use any code as `mj imagine \"...\" --sref CODE`)\n", len(items))
	return nil
}

// cmdProfile: mj profile USERNAME [--limit N] [--download DIR] [--format/--size]
func cmdProfile(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("profile", flag.ExitOnError)
	limit := fs.Int("limit", 50, "max results to show")
	download := fs.String("download", "", "download result images to dir")
	format := fs.String("format", "png", "png|webp")
	size := fs.Int("size", 0, "thumbnail edge px; 0 = full-res")
	jsonOut := fs.Bool("json", false, "")
	pos := parseArgs(fs, args)
	username, err := firstPos(pos, "profile USERNAME")
	if err != nil {
		return err
	}
	opts, err := assetOpts(*format, *size)
	if err != nil {
		return err
	}
	c, _, err := openClient(ctx, false)
	if err != nil {
		return err
	}
	defer c.Close()
	items, err := c.ProfileFeed(ctx, username)
	if err != nil {
		return err
	}
	return emitExplore(ctx, c, items, *limit, *download, opts, *jsonOut)
}

// cmdUploads: mj uploads [--download DIR] — list (and optionally pull) your
// uploaded assets in personal storage.
func cmdUploads(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("uploads", flag.ExitOnError)
	download := fs.String("download", "", "download uploaded assets to dir")
	jsonOut := fs.Bool("json", false, "")
	parseArgs(fs, args)
	c, _, err := openClient(ctx, false)
	if err != nil {
		return err
	}
	defer c.Close()
	ups, err := c.Uploads(ctx)
	if err != nil {
		return err
	}
	if *jsonOut {
		printJSON(ups)
	} else {
		for _, u := range ups {
			fmt.Printf("%s  %s\n", u.URL, orDash(u.ContentType))
		}
		fmt.Printf("(%d uploads)\n", len(ups))
	}
	if *download != "" {
		var n int
		for _, u := range ups {
			name := filepath.Base(u.BucketPathname)
			if name == "" || name == "." {
				name = fmt.Sprintf("upload_%d", u.TimeCreated)
			}
			if filepath.Ext(name) == "" { // ensure a sensible extension
				name += extForContentType(u.ContentType)
			}
			if err := c.SaveURL(ctx, u.URL, filepath.Join(*download, name)); err != nil {
				fmt.Fprintln(os.Stderr, "download", u.URL, "failed:", err)
				continue
			}
			n++
		}
		fmt.Printf("downloaded %d uploads to %s\n", n, *download)
	}
	return nil
}

// cmdLikedStyles: mj liked-styles — your liked style references (raw JSON).
func cmdLikedStyles(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("liked-styles", flag.ExitOnError)
	parseArgs(fs, args)
	c, _, err := openClient(ctx, false)
	if err != nil {
		return err
	}
	defer c.Close()
	b, err := c.LikedStyles(ctx)
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

// cmdFolders: mj folders — your folders/collections (raw JSON).
func cmdFolders(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("folders", flag.ExitOnError)
	parseArgs(fs, args)
	c, _, err := openClient(ctx, false)
	if err != nil {
		return err
	}
	defer c.Close()
	b, err := c.Folders(ctx)
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

// cmdProfiles: mj profiles — your personalization profiles (raw JSON). The ids
// here are usable as `mj imagine "..." --profile <id>`. (For a creator's public
// gallery, use `mj profile USERNAME`.)
func cmdProfiles(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("profiles", flag.ExitOnError)
	parseArgs(fs, args)
	c, _, err := openClient(ctx, false)
	if err != nil {
		return err
	}
	defer c.Close()
	b, err := c.PersonalizedProfiles(ctx)
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

// extForContentType maps a storage content-type label (e.g. "PNG", "image/jpeg")
// to a file extension.
func extForContentType(ct string) string {
	switch strings.ToLower(ct) {
	case "png", "image/png":
		return ".png"
	case "webp", "image/webp":
		return ".webp"
	case "gif", "image/gif":
		return ".gif"
	case "jpeg", "jpg", "image/jpeg":
		return ".jpg"
	default:
		return ".bin"
	}
}

func cmdQueue(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("queue", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "")
	parseArgs(fs, args)
	c, _, err := openClient(ctx, false)
	if err != nil {
		return err
	}
	defer c.Close()
	q, err := c.Queue(ctx)
	if err != nil {
		return err
	}
	if *jsonOut {
		printJSON(q)
		return nil
	}
	fmt.Printf("running: %d\n", len(q.Running))
	for _, j := range q.Running {
		fmt.Printf("  %s  %s  %s\n", j.ID, orDash(string(j.Status)), truncate(j.FullCommand, 60))
	}
	fmt.Printf("waiting: %d\n", len(q.Waiting))
	for _, j := range q.Waiting {
		fmt.Printf("  %s  %s\n", j.ID, truncate(j.FullCommand, 60))
	}
	return nil
}

func cmdMoodboards(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("moodboards", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "")
	parseArgs(fs, args)
	c, _, err := openClient(ctx, false)
	if err != nil {
		return err
	}
	defer c.Close()
	mb, err := c.Moodboards(ctx)
	if err != nil {
		return err
	}
	if *jsonOut {
		printJSON(mb)
		return nil
	}
	for _, m := range mb {
		fmt.Printf("%s  %q  images=%d  personalize=%v\n", m.ID, m.Title, m.ImageCount, m.Personalize)
	}
	fmt.Printf("(%d moodboards)\n", len(mb))
	return nil
}
