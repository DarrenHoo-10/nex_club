package rss

import (
	"bytes"
	"encoding/xml"
	"strings"
	"time"
)

type parsedFeed struct {
	LastBuild string
	Items     []parsedItem
}

type parsedItem struct {
	Title       string
	Link        string
	GUID        string
	Permalink   bool
	Description string
	Content     string
	Published   string
	Author      string
}

func parseFeed(data []byte) (parsedFeed, error) {
	var rssDoc struct {
		Channel struct {
			LastBuild string `xml:"lastBuildDate"`
			Items     []struct {
				Title       string `xml:"title"`
				Link        string `xml:"link"`
				GUID        guidEl `xml:"guid"`
				PubDate     string `xml:"pubDate"`
				Description string `xml:"description"`
				Encoded     string `xml:"http://purl.org/rss/1.0/modules/content/ encoded"`
				Author      string `xml:"author"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal(data, &rssDoc); err != nil {
		return parsedFeed{}, err
	}
	if len(rssDoc.Channel.Items) > 0 || strings.Contains(string(data), "<rss") || strings.Contains(string(data), "<item") {
		feed := parsedFeed{LastBuild: strings.TrimSpace(rssDoc.Channel.LastBuild)}
		for _, it := range rssDoc.Channel.Items {
			feed.Items = append(feed.Items, parsedItem{
				Title:       strings.TrimSpace(it.Title),
				Link:        strings.TrimSpace(it.Link),
				GUID:        strings.TrimSpace(it.GUID.Value),
				Permalink:   strings.EqualFold(strings.TrimSpace(it.GUID.IsPermaLink), "true"),
				Description: strings.TrimSpace(it.Description),
				Content:     strings.TrimSpace(it.Encoded),
				Published:   strings.TrimSpace(it.PubDate),
				Author:      strings.TrimSpace(it.Author),
			})
		}
		if len(feed.Items) > 0 || bytes.Contains(bytes.ToLower(data), []byte("<rss")) {
			return feed, nil
		}
	}
	var atom struct {
		Updated string `xml:"updated"`
		Entries []struct {
			Title     string `xml:"title"`
			ID        string `xml:"id"`
			Updated   string `xml:"updated"`
			Published string `xml:"published"`
			Summary   string `xml:"summary"`
			Content   string `xml:"content"`
			Author    struct {
				Name string `xml:"name"`
			} `xml:"author"`
			Links []struct {
				Href string `xml:"href,attr"`
				Rel  string `xml:"rel,attr"`
			} `xml:"link"`
		} `xml:"entry"`
	}
	if err := xml.Unmarshal(data, &atom); err != nil {
		return parsedFeed{}, err
	}
	feed := parsedFeed{LastBuild: strings.TrimSpace(atom.Updated)}
	for _, entry := range atom.Entries {
		link := ""
		for _, l := range entry.Links {
			if strings.EqualFold(l.Rel, "enclosure") {
				continue
			}
			if link == "" || strings.EqualFold(l.Rel, "alternate") || l.Rel == "" {
				if l.Href != "" && (link == "" || strings.EqualFold(l.Rel, "alternate")) {
					link = l.Href
				}
			}
		}
		published := strings.TrimSpace(entry.Published)
		if published == "" {
			published = strings.TrimSpace(entry.Updated)
		}
		feed.Items = append(feed.Items, parsedItem{
			Title:       strings.TrimSpace(entry.Title),
			Link:        strings.TrimSpace(link),
			GUID:        strings.TrimSpace(entry.ID),
			Description: strings.TrimSpace(entry.Summary),
			Content:     strings.TrimSpace(entry.Content),
			Published:   published,
			Author:      strings.TrimSpace(entry.Author.Name),
		})
	}
	return feed, nil
}

type guidEl struct {
	Value       string `xml:",chardata"`
	IsPermaLink string `xml:"isPermaLink,attr"`
}

func parseTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		time.RFC1123Z,
		time.RFC1123,
		time.RFC822Z,
		time.RFC822,
		"Mon, 02 Jan 2006 15:04:05 -0700",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}
