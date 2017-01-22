package ghostToHugo

import (
	"encoding/json"
	"io"
	"time"
)

// GhostToHugo handles the imprt of a Ghot blog export and outputting to
// hugo static blog
type GhostToHugo struct {
	location   *time.Location
	dateformat string
}

// Post is a blog post in Ghost
type Post struct {
	ID              int             `json:"id"`
	Title           string          `json:"title"`
	Slug            string          `json:"slug"`
	Content         string          `json:"markdown"`
	Image           string          `json:"image"`
	Page            json.RawMessage `json:"page"`
	Status          string          `json:"status"`
	MetaDescription string          `json:"meta_description"`
	AuthorID        int             `json:"author_id"`
	PublishedAt     json.RawMessage `json:"published_at"`
	CreatedAt       json.RawMessage `json:"created_at"`

	Published time.Time
	Created   time.Time
	IsDraft   bool
	IsPage    bool
	Author    string
	Tags      []string
}

func (p *Post) populate(gi *ghostInfo, gth *GhostToHugo) {
	p.Published = gth.parseTime(p.PublishedAt)
	p.Created = gth.parseTime(p.CreatedAt)
	p.IsDraft = p.Status == "draft"
	p.IsPage = parseBool(p.Page)

	for _, user := range gi.users {
		if user.ID == p.AuthorID {
			p.Author = user.Name
			break
		}
	}

	for _, pt := range gi.posttags {
		if pt.PostID == p.ID {
			for _, t := range gi.tags {
				if t.ID == pt.TagID {
					p.Tags = append(p.Tags, t.Name)
					break
				}
			}
		}
	}
}

func parseBool(rm json.RawMessage) bool {
	var b bool
	if err := json.Unmarshal(rm, &b); err == nil {
		return b
	}

	var i int
	if err := json.Unmarshal(rm, &i); err == nil {
		return i != 0
	}

	return false
}

type meta struct {
	ExportedOn int64  `json:"exported_on"`
	Version    string `json:"version"`
}

type user struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type tag struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type posttag struct {
	ID        int `json:"id"`
	PostID    int `json:"post_id"`
	TagID     int `json:"tag_id"`
	SortOrder int `json:"sort_order,omitempty"`
}

type ghostInfo struct {
	m        meta
	users    []user
	tags     []tag
	posttags []posttag
}

// WithLocation sets the location used when working with timestamps
func WithLocation(location *time.Location) func(*GhostToHugo) {
	return func(gth *GhostToHugo) {
		gth.location = location
	}
}

// WithDateFormat sets the date format to use for ghost imports
func WithDateFormat(format string) func(*GhostToHugo) {
	return func(gth *GhostToHugo) {
		gth.dateformat = format
	}
}

// NewGhostToHugo returns a new instance of GhostToHugo
func NewGhostToHugo(options ...func(*GhostToHugo)) (*GhostToHugo, error) {
	gth := new(GhostToHugo)

	// set defaults
	gth.dateformat = time.RFC3339
	gth.location = time.Local

	for _, option := range options {
		option(gth)
	}

	return gth, nil
}

func seekTo(d *json.Decoder, token json.Token) error {
	var tok json.Token
	var err error
	for err == nil && tok != token {
		tok, err = d.Token()
	}
	return err
}

func decodeGhostInfo(r io.Reader) (ghostInfo, error) {
	var gi ghostInfo
	var decoder = json.NewDecoder(r)
	var doneCount int

	for doneCount < 4 {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return gi, err
		}

		switch tok {
		case "meta":
			err = decoder.Decode(&gi.m)
			doneCount++
		case "users":
			err = decoder.Decode(&gi.users)
			doneCount++
		case "tags":
			err = decoder.Decode(&gi.tags)
			doneCount++
		case "posts_tags":
			err = decoder.Decode(&gi.posttags)
			doneCount++
		}

		if err != nil {
			return gi, err
		}
	}

	return gi, nil
}

func (gth *GhostToHugo) importGhost(r io.ReadSeeker) (<-chan Post, error) {

	gi, err := decodeGhostInfo(r)
	if err != nil {
		return nil, err
	}

	_, err = r.Seek(0, io.SeekStart)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(r)
	err = seekTo(decoder, "posts")
	if err != nil {
		return nil, err
	}
	_, err = decoder.Token() // Strip Token
	if err != nil {
		return nil, err
	}

	posts := make(chan Post)
	go func(decoder *json.Decoder, posts chan Post) {
		for decoder.More() {
			var p Post
			err = decoder.Decode(&p)
			if err != nil {
				break
			}
			p.populate(&gi, gth)
			posts <- p
		}
		close(posts)
	}(decoder, posts)

	return posts, nil
}

func (gth *GhostToHugo) parseTime(raw json.RawMessage) time.Time {
	var pt int64
	if err := json.Unmarshal(raw, &pt); err == nil {
		return time.Unix(0, pt*int64(time.Millisecond)).In(gth.location)
	}

	var ps string
	if err := json.Unmarshal(raw, &ps); err == nil {
		t, err := time.ParseInLocation(gth.dateformat, ps, gth.location)
		if err != nil {
			return time.Time{}
		}
		return t
	}

	return time.Time{}
}