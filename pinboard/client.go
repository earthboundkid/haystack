package pinboard

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	isatty "github.com/mattn/go-isatty"
	"github.com/mgutz/ansi"
)

func CLI(args []string) error {
	const tokenname = "PINBOARD_TOKEN"
	var (
		search            bool
		user, pass, token string
	)
	fl := flag.NewFlagSet("haystack", flag.ContinueOnError)
	fl.BoolVar(&search, "tag-search", false,
		`search for similar tags, rather than saved pages`)
	fl.BoolVar(&search, "t", false, "shortcut for -tag-search")
	fl.DurationVar(&http.DefaultClient.Timeout, "timeout", 5*time.Second,
		"timeout for query")
	fl.StringVar(&user, "user", "", "username")
	fl.StringVar(&pass, "password", "", "password")
	fl.StringVar(&token, "auth-token", "$"+tokenname,
		`auth token, see https://pinboard.in/settings/password`)
	fl.Usage = func() {
		fmt.Fprintf(fl.Output(), `haystack - a Pinboard search client

usage:

	haystack [options] <tags>...

-auth-token taken from environmental variable $PINBOARD_TOKEN if set.

Options:

`)
		fl.PrintDefaults()
	}
	if err := fl.Parse(args); err != nil {
		return flag.ErrHelp
	}
	cl := NewClient()
	if user == "" && pass == "" {
		if token == "$"+tokenname {
			token = os.Getenv(tokenname)
		}
		cl.SetToken(token)
	} else {
		cl.SetUsernamePassword(user, pass)
	}
	tags := fl.Args()
	if search {
		return cl.SearchTags(os.Stdout, tags)
	}
	return cl.SearchPosts(os.Stdout, tags)
}

type Client struct {
	BaseURL *url.URL
}

func NewClient() Client {
	u, _ := url.Parse("https://api.pinboard.in/?format=json")
	return Client{
		BaseURL: u,
	}
}

func (cl *Client) SetToken(token string) {
	q := cl.BaseURL.Query()
	q.Set("auth_token", token)
	cl.BaseURL.RawQuery = q.Encode()
}

func (cl *Client) SetUsernamePassword(u, p string) {
	cl.BaseURL.User = url.UserPassword(u, p)
}

func (cl Client) SearchTags(out io.Writer, tags []string) error {
	tagcounts, err := cl.TagsLike(tags...)
	if err != nil {
		return err
	}
	for _, tag := range tagcounts {
		fmt.Fprintln(out, tag)
	}
	return nil
}

func (cl Client) TagsLike(tags ...string) ([]TagCount, error) {
	canonicalTags, err := cl.GetTags()
	if err != nil {
		return nil, err
	}
	var returnTags []TagCount
	if len(tags) > 0 {
		normalizedTags := make([]string, len(tags))
		for i := range tags {
			normalizedTags[i] = strings.ToLower(tags[i])
		}
		for ctag, n := range canonicalTags {
			for _, ntag := range normalizedTags {
				if cntag := strings.ToLower(ctag); strings.Contains(cntag, ntag) {
					returnTags = append(returnTags, TagCount{ctag, n})
				}
			}
		}
		// Return all tags
	} else {
		returnTags = make([]TagCount, 0, len(canonicalTags))
		for ctag, n := range canonicalTags {
			returnTags = append(returnTags, TagCount{ctag, n})
		}
	}
	sort.Slice(returnTags, func(i, j int) bool {
		return returnTags[i].Count > returnTags[j].Count
	})
	return returnTags, nil
}

type TagCount struct {
	Tag   string
	Count int
}

func (tc TagCount) String() string {
	return fmt.Sprintf("%q: %d", tc.Tag, tc.Count)
}

func (cl Client) GetTags() (map[string]int, error) {
	raw := map[string]string{}
	if err := cl.Query("/v1/tags/get", nil, &raw); err != nil {
		return nil, err
	}
	data := make(map[string]int, len(raw))
	for k, v := range raw {
		i, _ := strconv.Atoi(v)
		data[k] = i
	}
	return data, nil
}

func (cl Client) SearchPosts(out io.Writer, tags []string) error {
	posts, err := cl.GetPosts(tags)
	if err != nil {
		return err
	}
	sort.Slice(posts, func(i, j int) bool {
		return posts[i].Time.Before(posts[j].Time)
	})
	return Template.Execute(os.Stdout, posts)
}

func (cl Client) GetPosts(tags []string) ([]Post, error) {
	q := url.Values{}
	for _, tag := range tags {
		q.Add("tag", tag)
	}
	var data RawAllPostsResponse
	if err := cl.Query("/v1/posts/all", q, &data); err != nil {
		return nil, err
	}
	return data.ToPosts(), nil
}

type RawAllPostsResponse []struct {
	Description string    `json:"description"`
	Extended    string    `json:"extended"`
	Hash        string    `json:"hash"`
	Href        string    `json:"href"`
	Meta        string    `json:"meta"`
	Shared      string    `json:"shared"`
	Tags        string    `json:"tags"`
	Time        time.Time `json:"time"`
	ToRead      string    `json:"toread"`
}

func (raw RawAllPostsResponse) ToPosts() []Post {
	posts := make([]Post, len(raw))
	for i := range raw {
		original := &raw[i]
		u, _ := url.Parse(original.Href)
		posts[i] = Post{
			Title:       original.Description,
			Description: original.Extended,
			Hash:        original.Hash,
			Tags:        strings.Fields(original.Tags),
			Time:        original.Time,
			URL:         u,
			Shared:      original.Shared == "yes",
			ToRead:      original.ToRead == "yes",
		}
	}
	return posts
}

type Post struct {
	Title, Description, Hash string
	Tags                     []string
	Time                     time.Time
	URL                      *url.URL
	Shared, ToRead           bool
}

func (cl Client) Query(path string, values url.Values, data interface{}) error {
	u := *cl.BaseURL
	u.Path = path
	q := u.Query()
	for key, vals := range values {
		q[key] = vals
	}
	u.RawQuery = q.Encode()
	rsp, err := http.Get(u.String())
	if err != nil {
		return err
	}
	defer rsp.Body.Close()
	if rsp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad response: %s", rsp.Status)
	}
	dec := json.NewDecoder(rsp.Body)
	return dec.Decode(&data)
}

var Template = template.Must(
	template.New("").
		Funcs(template.FuncMap{
			"bold": func(s string) string {
				if !isatty.IsTerminal(os.Stdout.Fd()) {
					return s
				}
				return ansi.Color(s, "red+b")
			},
			"underline": func(s string) string {
				if !isatty.IsTerminal(os.Stdout.Fd()) {
					return s
				}
				return ansi.Color(s, "white+u")
			},
		}).
		Parse(
			`
{{- range . -}}
Title: {{ .Title | bold }}{{ with .Description }}
Description: {{ . }}{{ end }}
Date: {{ .Time.Local.Format "Jan. 2, 2006 3:04pm" }}
Tags: {{range .Tags}}{{ . }} {{end}}
URL: {{ .URL.String | underline }}

{{ end -}}`,
		),
)
