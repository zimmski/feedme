package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strconv"

	"github.com/codegangsta/martini"
	"github.com/jessevdk/go-flags"
	"github.com/zimmski/feeds"

	"github.com/zimmski/feedme/backend"
)

const (
	RET_OK = iota
	RET_HELP
)

type FeedEnum int

const (
	FEED_ATOM FeedEnum = iota
	FEED_RSS
)

var opts struct {
	Logging      bool   `long:"enable-logging" description:"Enable request logging"`
	MaxIdleConns int    `long:"max-idle-conns" default:"10" description:"Max idle connections of the database"`
	MaxOpenConns int    `long:"max-open-conns" default:"10" description:"Max open connections of the database"`
	Port         uint   `short:"p" long:"port" default:"9090" description:"HTTP port of the server"`
	Spec         string `short:"s" long:"spec" default:"dbname=feedme sslmode=disable" description:"The database connection spec"`
}

var db backend.Backend

func checkError(res http.ResponseWriter, err error) bool {
	if err != nil {
		panic(err)
	}

	return false
}

func checkNotFound(res http.ResponseWriter, item interface{}) bool {
	if item == nil || !reflect.ValueOf(item).Elem().IsValid() {
		res.WriteHeader(http.StatusNotFound)

		return true
	}

	return false
}

func handleFeeds(res http.ResponseWriter, req *http.Request) {
	var err error

	feeds, err := db.SearchFeeds()
	if checkError(res, err) {
		return
	}

	data, err := json.Marshal(feeds)
	if checkError(res, err) {
		return
	}

	res.WriteHeader(http.StatusOK)
	res.Header().Set("Content-Type", "application/json")
	res.Write(data)
}

func getFeedItems(feedName string) (*feeds.Feed, error) {
	var err error

	feed, err := db.FindFeed(feedName)
	if err != nil {
		return nil, err
	}
	if feed == nil {
		return nil, nil
	}

	items, err := db.SearchItems(feed)
	if err != nil {
		return nil, err
	}
	if items == nil {
		return nil, nil
	}

	feeder := &feeds.Feed{
		Title: feed.Name,
		Link:  &feeds.Link{Href: feed.Url},
	}

	for _, i := range items {
		if feeder.Updated.IsZero() || feeder.Updated.Before(i.Created) {
			feeder.Updated = i.Created
		}

		feeder.Add(&feeds.Item{
			Id:          strconv.Itoa(i.Id),
			Title:       i.Title,
			Link:        &feeds.Link{Href: fmt.Sprintf("%s%s", feed.Url, i.Uri)},
			Description: i.Description,
			Created:     i.Created,
		})
	}

	return feeder, nil
}

func handleItems(typ FeedEnum, res http.ResponseWriter, req *http.Request, params martini.Params) {
	var err error

	feeder, err := getFeedItems(params["feed"])
	if checkError(res, err) {
		return
	}
	if checkNotFound(res, feeder) {
		return
	}

	var data string

	if typ == FEED_ATOM {
		data, err = feeder.ToAtom()
	} else {
		data, err = feeder.ToRss()
	}
	if checkError(res, err) {
		return
	}

	res.WriteHeader(http.StatusOK)
	res.Header().Set("Content-Type", "application/xml")
	res.Write([]byte(data))
}

func handleItemsAtom(res http.ResponseWriter, req *http.Request, params martini.Params) {
	handleItems(FEED_ATOM, res, req, params)
}

func handleItemsRss(res http.ResponseWriter, req *http.Request, params martini.Params) {
	handleItems(FEED_RSS, res, req, params)
}

func main() {
	var err error

	p := flags.NewNamedParser("feedme-server", flags.HelpFlag)
	p.ShortDescription = "The feedme server"
	p.AddGroup("Server arguments", "", &opts)

	_, err = p.ParseArgs(os.Args)
	if err != nil {
		if e, ok := err.(*flags.Error); !ok || e.Type != flags.ErrHelp {
			panic(err)
		} else {
			p.WriteHelp(os.Stdout)

			os.Exit(RET_HELP)
		}
	}

	db, err = backend.NewBackend("postgresql")
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

	ma := martini.New()

	if opts.Logging {
		ma.Use(martini.Logger())
	}
	ma.Use(martini.Recovery())

	r := martini.NewRouter()
	ma.Action(r.Handle)

	m := martini.ClassicMartini{ma, r}

	m.Get("/", handleFeeds)
	m.Get("/:feed/atom", handleItemsAtom)
	m.Get("/:feed/rss", handleItemsRss)

	http.ListenAndServe(fmt.Sprintf(":%d", opts.Port), m)

	os.Exit(RET_OK)
}
