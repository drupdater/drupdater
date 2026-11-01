# Agent protocol fixtures

One example of every request and reply the agent exchanges with drupdater. The agent's PHPUnit
suite asserts it produces each reply; `internal/agent` and `internal/composer` assert drupdater
decodes them. Change a shape here and both sides must follow, or a suite fails.
