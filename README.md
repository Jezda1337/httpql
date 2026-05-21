# HTTPQL

> psql for HTTP APIs.

HTTPQL is a psql-inspired interactive shell for exploring and working with HTTP APIs from the terminal.

```sh
$ hreq

hreq> \set host http://localhost:6969
hreq> \set token abc123
hreq> \header Authorization Bearer {{token}}

hreq> get /users

---------
GET http://localhost:6969/users
Status: 200 OK
---------

[
  {
    "id": 69,
    "name": "john doe"
  }
]
```

## Build

```sh
git clone https://github.com/jezda1337/httpql
cd httpql
go build -o hreq main.go
```

## Philosophy

HTTPQL treats HTTP APIs like a queryable session, similar to `psql` for databases.
