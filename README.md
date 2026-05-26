# HTTPQL

> psql for HTTP APIs.

HTTPQL is a psql-inspired interactive shell for exploring and working with HTTP APIs from the terminal.

```sh
$ httpql

» \set host http://localhost:6969
» \set token abc123
» \header Authorization Bearer {{token}}
» \q # exit

» get /users; # ; trigger the request
# basically ; means run this query

# example on how can we send the JSON
» post /users/69 {
> "name": "John Doe",
> };

# example on how to use variables
» \set userID 69
» get /users/{{userID}};

# example on how to use pipe with `jq` to extract JSON
# this will return just name ignoring rest of the response
» get /users/{{userID}} | jq .name

# example on how to use/save session
# sessions are stored in $HOME/.httpql/
# saved sessions holds all data you had set first, host, vars, headers, does not save history
# using saved session loads host, vars and headers
» \sessions # print all sessions
» \session save session-name
» \session use session-name

» \env # print current session data

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
