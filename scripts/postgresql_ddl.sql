/* Settings */


/* Drops */

DROP TABLE IF EXISTS items;
DROP TABLE IF EXISTS feeds;

/* Tables */

CREATE TABLE feeds (
	id SERIAL,
	name TEXT NOT NULL,
	url TEXT NOT NULL,
	transform TEXT NOT NULL,
	PRIMARY KEY(id),
	UNIQUE(name)
);

CREATE TABLE items (
	feed INTEGER NOT NULL,
	id SERIAL,
	title TEXT NOT NULL,
	uri TEXT NOT NULL,
	description TEXT NOT NULL,
	created TIMESTAMP NOT NULL,
	PRIMARY KEY(id)
);

/* new Settings */

/* Foreign Keys */

ALTER TABLE items
	ADD CONSTRAINT items_feed_fk
	FOREIGN KEY(feed)
	REFERENCES feeds(id)
	ON DELETE CASCADE;

/* Indizes */
