package workflow

import "encoding/json"

// siteFacts are a site's facts where the test does not read them: the runner hands them on unread.
var siteFacts = json.RawMessage(`{"modules":["system"]}`)
