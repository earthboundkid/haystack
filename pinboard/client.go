package pinboard

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"text/template"
	"time"
)

func CLI(args []string) error {
	fl := flag.NewFlagSet("haystack", flag.ContinueOnError)
	fl.Usage = func() {
		fmt.Fprintf(fl.Output(), `haystack - a Pinboard search client

usage:

	haystack [options] <tags>...

Expects environmental variable PINBOARD_TOKEN set from https://pinboard.in/settings/password
`)
		fl.PrintDefaults()
	}
	if err := fl.Parse(args); err != nil {
		return flag.ErrHelp
	}

	cl := NewClient()
	posts, err := cl.GetPosts(fl.Args())
	if err != nil {
		return err
	}
	return Template.Execute(os.Stdout, posts)
}

type Client struct {
	BaseURL *url.URL
}

func NewClient() Client {
	token := os.Getenv("PINBOARD_TOKEN")
	u, _ := url.Parse("https://api.pinboard.in/?format=json&auth_token=" + token)
	return Client{
		BaseURL: u,
	}
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

	dec := json.NewDecoder(rsp.Body)
	return dec.Decode(&data)
}

var Template = template.Must(template.New("").Parse(`
{{- range . -}}
Title: {{ .Title }}: {{ .Description }}
Date: {{ .Time.Local.Format "Jan. 2, 2006 3:04pm" }}
Tags: {{range .Tags}}{{ . }} {{end}}
URL: {{ .URL }}

{{ end -}}
`))
