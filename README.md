# HTTPQL

> psql for HTTP APIs.

HTTPQL is a psql-inspired interactive shell for exploring and working with HTTP APIs from the terminal.

```sh
# for simple testing we can use program this way (order is not matter) (work in progress)
$ httpql jsonplaceholder.typicode.com/users/1 -H "Content-Type: application/json" --json '{"...": "..."}' -M POST | jq ...

$ httpql # interactive mode

httpql> \set host http://localhost:6969
httpql> \set token abc123
httpql> \set verbose on # will show more details about request
httpql> \header Authorization Bearer {{token}}
httpql> \q # exit

httpql> get /users; # ; trigger the request
# basically ; means run this query

# example on how can we send the JSON
httpql> post /users/69 {
> "name": "John Doe",
> };

# example on how to use variables
httpql> \set userID 69
httpql> get /users/{{userID}};

# example on how to use pipe with `jq` to extract JSON
# this will return just name ignoring rest of the response
# httpql> get /users/{{userID}} | jq .name

# example on how to use/save session
# sessions are stored in $HOME/.httpql/
# saved sessions holds all data you had set first, host, vars, headers, does not save history
# using saved session loads host, vars and headers
httpql> \session list # print all sessions
httpql> \session save session-name
httpql> \session load session-name
httpql> \session delete session-name

httpql> \print # print current session data

---------
GET http://localhost:6969/users
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

## Install

```sh
make install # this cmd will build && move prgram to ~/.local/bin
```

## Philosophy

HTTPQL treats HTTP APIs like a queryable session, similar to `psql` for databases.
