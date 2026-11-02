# App protocol fixtures

One example of every request the Go binary sends the app and of the app's reply. The app's
PHPUnit suite asserts it produces each reply; `internal/composer` asserts the binary sends each
request and decodes each reply. The app's own requests to the agent are the agent's fixtures,
`agent/protocol/`. Change a shape here and both sides must follow, or a suite fails.
