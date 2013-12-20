package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"text/template"

	"github.com/PuerkitoBio/goquery"
	"github.com/jessevdk/go-flags"

	"github.com/zimmski/feedme"
	"github.com/zimmski/feedme/backend"
)

const (
	RET_OK = iota
	RET_HELP
)

var opts struct {
	MaxIdleConns int    `long:"max-idle-conns" default:"10" description:"Max idle connections of the database"`
	MaxOpenConns int    `long:"max-open-conns" default:"10" description:"Max open connections of the database"`
	Spec         string `short:"s" long:"spec" default:"dbname=feedme sslmode=disable" description:"The database connection spec"`
	Verbose      bool   `short:"v" long:"verbose" description:"Print what is going on"`
}

func main() {
	var err error

	p := flags.NewNamedParser("feedme-crawler", flags.HelpFlag)
	p.ShortDescription = "The feedme crawler"
	p.AddGroup("Crawler arguments", "", &opts)

	_, err = p.ParseArgs(os.Args)
	if err != nil {
		if e, ok := err.(*flags.Error); !ok || e.Type != flags.ErrHelp {
			panic(err)
		} else {
			p.WriteHelp(os.Stdout)

			os.Exit(RET_HELP)
		}
	}

	db, err := backend.NewBackend("postgresql")
	if err != nil {
		panic(err)
	}

	params := backend.BackendParameters{
		Spec:         opts.Spec,
		MaxIdleConns: opts.MaxIdleConns,
		MaxOpenConns: opts.MaxOpenConns,
	}

	err = db.Init(params)
	if err != nil {
		panic(err)
	}

	feeds, err := db.SearchFeeds()
	if err != nil {
		panic(err)
	}

	for _, feed := range feeds {
		if opts.Verbose {
			fmt.Printf("Fetch feed %s from %s\n", feed.Name, feed.Url)
		}

		var raw map[string]*json.RawMessage
		err = json.Unmarshal([]byte(feed.Transform), &raw)
		if err != nil {
			fmt.Printf("ERROR cannot parse transform JSON: %s\n", err.Error())

			continue
		}

		var transform map[string]string
		err = json.Unmarshal(*raw["transform"], &transform)
		if err != nil {
			fmt.Printf("ERROR cannot parse transform item: %s\n", err.Error())

			continue
		}

		transformTemplates := make(map[string]*template.Template)
		for name, tem := range transform {
			transformTemplates[name], err = template.New(name).Parse(tem)
			if err != nil {
				fmt.Printf("ERROR cannot create transform template: %s\n", err.Error())

				continue
			}
		}

		jsonItems, err := jsonArray(raw["items"])
		if err != nil {
			fmt.Printf("ERROR cannot parse items item: %s\n", err.Error())

			continue
		}

		doc, err := goquery.NewDocument(feed.Url)
		if err != nil {
			fmt.Printf("ERROR cannot open URL: %s\n", err.Error())

			continue
		}

		var items []feedme.Item

		for _, rawTransform := range jsonItems {
			item := make(map[string]interface{})

			err = crawlNode(doc.Selection, rawTransform, item)
			if err != nil {
				fmt.Printf("Cannot transform website: %s\n", err.Error())

				goto BADFEED
			}

			feedItem := feedme.Item{}

			for name, t := range transformTemplates {
				var out bytes.Buffer
				t.Execute(&out, item)
				s := out.String()

				switch name {
				case "description":
					feedItem.Description = s
				case "title":
					feedItem.Title = s
				case "uri":
					feedItem.Uri = s
				default:
					fmt.Printf("unkown field %s\n", name)

					goto BADFEED
				}
			}

			if feedItem.Title != "" && feedItem.Uri != "" {
				if opts.Verbose {
					fmt.Printf("\tFound item %+v\n", feedItem)
				}

				items = append(items, feedItem)
			}
		}

		err = db.CreateItems(&feed, items)
		if err != nil {
			fmt.Printf("ERROR cannot insert items into database: %s\n", err.Error())

			continue
		}

	BADFEED:
	}

	os.Exit(RET_OK)
}

func crawlNode(element *goquery.Selection, rawTransform map[string]*json.RawMessage, item map[string]interface{}) error {
	if raw, ok := rawTransform["search"]; ok {
		items, selector, err := jsonNode(raw)
		if err != nil {
			return err
		}

		element.Find(selector).Each(func(i int, s *goquery.Selection) {
			for _, i := range items {
				err = crawlNode(s, i, item)
				if err != nil {
					return
				}
			}
		})
		if err != nil {
			return err
		}
	} else if raw, ok := rawTransform["find"]; ok {
		items, selector, err := jsonNode(raw)
		if err != nil {
			return err
		}

		s := element.Find(selector)
		if s == nil {
			return errors.New("no item found")
		}

		for _, i := range items {
			err = crawlNode(s, i, item)
			if err != nil {
				return err
			}
		}
	} else if raw, ok := rawTransform["attr"]; ok {
		items, selector, err := jsonNode(raw)
		if err != nil {
			return err
		}

		attr, ok := element.Attr(selector)
		if !ok {
			return errors.New("no attr found")
		}

		for _, i := range items {
			err = crawlAttr(attr, i, item)
			if err != nil {
				return err
			}
		}
	} else {
		return errors.New(fmt.Sprintf("do not know how to transform Node %+v", rawTransform))
	}

	return nil
}

func crawlAttr(attr string, rawTransform map[string]*json.RawMessage, item map[string]interface{}) error {
	var err error

	if raw, ok := rawTransform["regex"]; ok {
		var transformMatches []map[string]string
		err = json.Unmarshal(*rawTransform["data"], &transformMatches)
		if err != nil {
			return err
		}

		reg, err := jsonString(raw)
		if err != nil {
			return err
		}

		re := regexp.MustCompile(reg)
		var matches = re.FindStringSubmatch(attr)

		if matches == nil {
			return errors.New("no matches found")
		}

		if len(matches)-1 != len(transformMatches) {
			return errors.New("unequal match count")
		}

		for i := 0; i < len(transformMatches); i++ {
			var name = transformMatches[i]["name"]

			switch transformMatches[i]["type"] {
			case "int":
				v, _ := strconv.Atoi(matches[i+1])

				item[name] = v
			case "string":
				item[name] = matches[i+1]
			default:
				return errors.New(fmt.Sprintf("type %s not found", transformMatches[i]["type"]))
			}
		}
	} else {
		return errors.New(fmt.Sprintf("do not know how to transform Attrs %+v", rawTransform))
	}

	return nil
}

func jsonArray(raw *json.RawMessage) ([]map[string]*json.RawMessage, error) {
	var array []map[string]*json.RawMessage

	err := json.Unmarshal(*raw, &array)
	if err != nil {
		return nil, err
	}

	return array, nil
}

func jsonHash(raw *json.RawMessage) (map[string]*json.RawMessage, error) {
	var hash map[string]*json.RawMessage

	err := json.Unmarshal(*raw, &hash)
	if err != nil {
		return nil, err
	}

	return hash, nil
}

func jsonString(raw *json.RawMessage) (string, error) {
	var s string

	err := json.Unmarshal(*raw, &s)
	if err != nil {
		return "", err
	}

	return s, nil
}

func jsonNode(raw *json.RawMessage) ([]map[string]*json.RawMessage, string, error) {
	node, err := jsonHash(raw)
	if err != nil {
		return nil, "", err
	}

	items, err := jsonArray(node["do"])
	if err != nil {
		return nil, "", err
	}

	selector, err := jsonString(node["selector"])
	if err != nil {
		return nil, "", err
	}

	return items, selector, nil
}
