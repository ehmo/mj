package mjclient

import (
	"context"
	"fmt"
	"net/url"

	"github.com/ehmo/mj/internal/mjapi"
)

// Search runs a semantic vector search over the public explore gallery.
// page is 1-based (matching the web app). Returns published community images.
func (c *Client) Search(ctx context.Context, query string, page int) ([]mjapi.ExploreItem, error) {
	if page < 1 {
		page = 1
	}
	p := fmt.Sprintf("/api/explore-vector-search?prompt=%s&page=%d&_ql=explore", url.QueryEscape(query), page)
	b, err := c.Get(ctx, p)
	if err != nil {
		return nil, err
	}
	return mjapi.ParseExplore(b)
}

// ExploreFeed selects the explore browse feed. Values confirmed live from the
// web app's feed selector.
type ExploreFeed string

const (
	FeedTop      ExploreFeed = "top"       // top all-time
	FeedTopWeek  ExploreFeed = "top_week"  // top this week
	FeedTopMonth ExploreFeed = "top_month" // top this month
	FeedHot      ExploreFeed = "hot"       // trending
	FeedRandom   ExploreFeed = "random"
	FeedVideos   ExploreFeed = "videos" // community videos
)

// ExploreFeeds lists the valid feed values.
var ExploreFeeds = []ExploreFeed{FeedTop, FeedTopWeek, FeedTopMonth, FeedHot, FeedRandom, FeedVideos}

// ValidFeed reports whether f is a known explore feed.
func ValidFeed(f ExploreFeed) bool {
	for _, v := range ExploreFeeds {
		if v == f {
			return true
		}
	}
	return false
}

// Explore browses the public explore gallery (page is 0-based, matching the
// web app's explore feed).
func (c *Client) Explore(ctx context.Context, feed ExploreFeed, page int) ([]mjapi.ExploreItem, error) {
	if feed == "" {
		feed = FeedTop
	}
	if !ValidFeed(feed) {
		return nil, fmt.Errorf("unknown feed %q (valid: %v)", feed, ExploreFeeds)
	}
	if page < 0 {
		page = 0
	}
	p := fmt.Sprintf("/api/explore?feed=%s&page=%d&_ql=explore", feed, page)
	b, err := c.Get(ctx, p)
	if err != nil {
		return nil, err
	}
	return mjapi.ParseExplore(b)
}

// Styles browses the public style-reference gallery (explore-srefs). Each result
// carries an sref code usable as `--sref <code>`. page is 0-based.
func (c *Client) Styles(ctx context.Context, page int) ([]mjapi.ExploreItem, error) {
	if page < 0 {
		page = 0
	}
	p := fmt.Sprintf("/api/explore-srefs?page=%d&_ql=explore&feed=styles_random", page)
	b, err := c.Get(ctx, p)
	if err != nil {
		return nil, err
	}
	return mjapi.ParseExplore(b)
}

// Likes returns the images the account has liked. page is 1-based, matching the
// web app (explore-likes rejects page=0 with 422).
func (c *Client) Likes(ctx context.Context, page int) ([]mjapi.ExploreItem, error) {
	if page < 1 {
		page = 1
	}
	p := fmt.Sprintf("/api/explore-likes?page=%d&_ql=explore", page)
	b, err := c.Get(ctx, p)
	if err != nil {
		return nil, err
	}
	return mjapi.ParseExplore(b)
}

// ProfileFeed returns a user's published spotlight gallery by username.
func (c *Client) ProfileFeed(ctx context.Context, username string) ([]mjapi.ExploreItem, error) {
	if username == "" {
		return nil, fmt.Errorf("profile: username required")
	}
	p := fmt.Sprintf("/api/spotlight-feed?username_v2=%s", url.QueryEscape(username))
	b, err := c.Get(ctx, p)
	if err != nil {
		return nil, err
	}
	return mjapi.ParseExplore(b)
}

// Uploads lists the account's uploaded assets in personal storage.
func (c *Client) Uploads(ctx context.Context) ([]mjapi.Upload, error) {
	b, err := c.Get(ctx, "/api/storage")
	if err != nil {
		return nil, err
	}
	return mjapi.ParseStorage(b)
}

// LikedStyles returns the raw `/api/explore-styles-likes` JSON (style objects;
// shape surfaced verbatim).
func (c *Client) LikedStyles(ctx context.Context) ([]byte, error) {
	return c.Get(ctx, "/api/explore-styles-likes?_ql=explore")
}

// Queue returns the account's running and waiting jobs.
func (c *Client) Queue(ctx context.Context) (mjapi.Queue, error) {
	b, err := c.Get(ctx, "/api/user-queue")
	if err != nil {
		return mjapi.Queue{}, err
	}
	return mjapi.ParseQueue(b)
}

// Moodboards lists the account's personalization moodboards.
func (c *Client) Moodboards(ctx context.Context) ([]mjapi.Moodboard, error) {
	b, err := c.Get(ctx, "/api/moodboards")
	if err != nil {
		return nil, err
	}
	return mjapi.ParseMoodboards(b)
}

// Folders returns the raw `/api/folders` JSON (collection structure varies; the
// shape is surfaced verbatim for now).
func (c *Client) Folders(ctx context.Context) ([]byte, error) {
	return c.Get(ctx, "/api/folders")
}

// PersonalizedProfiles returns the raw `/api/personalized-profiles` JSON.
func (c *Client) PersonalizedProfiles(ctx context.Context) ([]byte, error) {
	return c.Get(ctx, "/api/personalized-profiles")
}
