# feedme

feedme is an infrastructure for creating Atom and RSS feeds from any website. It consists of a crawler and a web service. The crawler fetches feed definitions like the website URL and what to fetch from the website out of a database backend, crawls the website, transforms the crawled information into consistent feed items and stores them in the database. The web service generates a valid Atom and RSS feed using these items of a given feed.

## Requirements

* Go 1.2 or higher
* PostgreSQL as the database backend

## Set up feedme

*Please note that the following commands use the users's default PostgreSQL user, database and password. If you want to use different login settings you have to specify them using the corresponding psql, [feedme-crawler](#feedme-crawler) and [feedme-server](#feedme-server) arguments*

Fetch feedme with the go command and install all dependencies.

```bash
go get github.com/zimmski/feedme
cd $GOPATH/src/github.com/zimmski/feedme
go get ./...
```

Initialize the database backend. Make sure that this works without errors.

```bash
psql < $GOPATH/src/github.com/zimmski/feedme/scripts/postgresql_ddl.sql
```

Create binaries for the crawler and server.

```bash
go install github.com/zimmski/feedme/feedme-crawler
go install github.com/zimmski/feedme/feedme-server
```

*Please note that you could also use just the usual <code>go run</code> to start the crawler or server.*

Start the server

```bash
$GOBIN/feedme-server --enable-logging
```

Insert your feeds with the transformation definitions into the database and execute the crawler for the first time. Make sure that this works without errors.

```bash
$GOBIN/feedme-crawler --verbose
```

Test your feeds with your RSS reader or browser by going to http://localhost:9090/, http://localhost:9090/yourfeedname/atom and http://localhost:9090/yourfeedname/rss. If everything works you can run the crawler as cron job to refresh your feeds automatically.

## Add feeds to the database

Currently there is no interface for inserting and editing feed definitions. You have to insert and update them manually through your favorite PostgreSQL interface. In the folder <code>/examples</code> you can find examples for transformations.

For example [/examples/dilbert.com.json](/examples/dilbert.com.json) holds the transformation for the divine Dilbert comic. This definition will add the current comic image of the home page of dilbert.com if it does not already exists in the database.

You can add the dilbert.com feed to your database by issuing the following SQL statement.

```SQL
INSERT INTO feeds(name, url, transform) VALUES ('dilbert.com', 'http://dilbert.com/', '{"items": [{"search": "div.STR_Image","do": [{"find": "a","do": [{"attr": "href","do": [{"regex": "/strips/comic/(.+)/","matches": [{"name": "date","type": "string"}]}]}]},{"find": "img","do": [{"attr": "src","do": [{"copy": true,"name": "image","type": "string"}]}]}]}],"transform": {"title": "Strip {{.date}}","uri": "/strips/comic/{{.date}}/","description": "<img src=\"http://dilbert.com{{.image}}\"/> Strip {{.date}}"}}');
```

The <code>name</code> column of the <code>feeds</code> table must be unique and states the identifying name of the feed for the feed URL of the web service. The <code>url</code> column defines which page should be fetched and transformed for the feed generation. The <code>transform</code> column holds the transform definition.

## Transformation (definition)

A transformation definition uses JSON as its format. The base consists of the two elements <code>items</code> (an array of selectors) and <code>transform</code> (a hash of templates for the feed item fields).

An empty transformation definition:

```json
{
	"items": [
	],
	"transform": {
	}
}
```

The <code>transform</code> hash holds key-value pairs of templates. For example the following transform hash would assign all found feed items the title "News title", the uri "/the/news/uri" and the description "This just in. An important news.":

```json
{
	"items": [
	],
	"transform": {
		"title": "News title",
		"uri": "/the/news/uri",
		"description": "This just in. An important news."
	}
}
```

A definition for the <code>transform</code> element does not make much sense without items that can be transformed. The <code>items</code> element defines selectors for selecting DOM elements from the feed's URL and also holds definitions on what information should be stored. Stored information can be accessed by the <code>transform</code> element through their identifiers.

For example
```json
{
	"items": [
	],
	"transform": {
		"title": "News {{.title}}",
		"uri": "/images/{{.image}}",
		"description": "The title {{.title}} belongs to the image {{.image}}."
	}
}
```
would access the stored informations of <code>title</code> and <code>image</code> for each feed item.

### Selecting nodes

Selecting nodes can be nested through their <code>do</code> element and can contain storing nodes.

**search**

Search uses a CSS selector to select many elements.

```json
{
	"search": "CSS selector",
	"do": [
	]
}
```

**find**

Find uses a CSS selector to select at most one element.

```json
{
	"find": "CSS selector",
	"do": [
	]
}
```

**attr**

Attr selects exactly one attribute of the parents element and can only contain storing nodes in its <code>do</code> element.

```json
{
	"search": "attribute name",
	"do": [
	]
}
```

**text**

Text extracts the combined text contents of the current node and its children.

```json
{
	"text": true,
	"do": [
	]
}
```

### Storing nodes

**copy**

Copy copies the attribute value direclty for the feed item transformation.

```json
{
	"copy": true,
	"name": "storing name",
	"type": "int or string, which is the type of the value"
}
```

**regex**

Regex uses its regex string on the parents attribute value to parse it and store matching groups for the feed item transformation. The <code>matches</code> element holds an array of name-type pairs for storing item information and must match the count of the matching groups of the regex.

```json
{
	"regex": "regex with capturing groups",
	"matches": [
		{
			"name": "storing name of first match",
			"type": "int or string, which is the type of the value"
		}
	]
}
```

For example

```json
{
	"regex": "id=(\\d+)&image=(.+)",
	"matches": [
		{
			"name": "id",
			"type": "int"
		},
		{
			"name": "image",
			"type": "string"
		}
	]
}
```

would parse the value of the given attribute and store the parsed values into <code>id</code> and <code>image</code> for transforming the feed items.

### Example file

```json
{
	"items": [
		{
			"search": "div.news",
			"do": [
				{
					"find": "a",
					"do": [
						{
							"attr": "href",
							"do": [
								{
									"regex": "id=(\\d+)",
									"matches": [
										{
											"name": "id",
											"type": "int"
										}
									]
								}
							]
						}
					]
				},
				{
					"find": "img",
					"do": [
						{
							"attr": "src",
							"do": [
								{
									"copy": true,
									"name": "image",
									"type": "string"
								}
							]
						}
					]
				}
			]
		}
	],
	"transform": {
		"title": "News {{.id}}",
		"uri": "/news/{{.id}}",
		"description": "<img src=\"{{.image}}\"/>"
	}
}
```

This transformation selects all <code>div.news</code> elements of the fetched page and looks into every <code>div.news</code> element for the first <code>a</code> element and <code>img</code> element. The <code>href</code> attribute of the <code>a</code> element gets parsed by a regex for the news id which is stored with the identifier <code>id</code>. The <code>src</code> attribute of the <code>img</code> element gets copied into the <code>image</code> identifier.

Every <code>div.news</code> elements represents a feed item as the selection for <code>div.news</code> elements is in the root array of the items transformation. All stored identifiers will be given to the templates of the fields in the transform hash. After inserting their information into the feed item field values the final values are stored into the database.

## feedme-crawler

**CLI arguments**

```
      --max-idle-conns= Max idle connections of the database (10)
      --max-open-conns= Max open connections of the database (10)
  -s, --spec=           The database connection spec (dbname=feedme sslmode=disable)
  -w, --workers=        Worker count for processing feeds (1)
  -v, --verbose         Print what is going on

  -h, --help            Show this help message
```

The <code>--spec</code> argument uses the connection string parameter of the excellent <code>pg</code> package. Please have a look at the [official documentation](http://godoc.org/github.com/lib/pq#hdr-Connection_String_Parameters) if you need different settings.

## feedme-server

**CLI arguments**

```
      --enable-logging  Enable request logging
      --max-idle-conns= Max idle connections of the database (10)
      --max-open-conns= Max open connections of the database (10)
  -p, --port=           HTTP port of the server (9090)
  -s, --spec=           The database connection spec (dbname=feedme sslmode=disable)

  -h, --help            Show this help message
```

The <code>--spec</code> argument uses the connection string parameter of the excellent <code>pg</code> package. Please have a look at the [official documentation](http://godoc.org/github.com/lib/pq#hdr-Connection_String_Parameters) if you need different settings.

**Routes**

* <code>/</code> - Displays all feed definitions via JSON.
* <code>/&lt;feed name&gt;/atom</code> - Displays an Atom feed for the given feed.
* <code>/&lt;feed name&gt;/rss</code> - Displays an RSS feed for the given feed.
