package templates

import (
	"strings"
	"testing"
)

func TestFillHTMLTemplate(t *testing.T) {
	content, err := FillHTMLTemplate("UserIdCard", Args{
		"id":     12345,
		"msg_id": "message-1",
	})
	if err != nil {
		t.Fatalf("FillHTMLTemplate returned an error: %v", err)
	}

	if !strings.Contains(content, "<dd>12345</dd>") || !strings.Contains(content, "<dd>message-1</dd>") {
		t.Fatalf("template variables were not filled: %q", content)
	}

	if _, err := FillHTMLTemplate("UserIdCard", Args{"id": 12345}); err == nil {
		t.Fatal("FillHTMLTemplate did not report a missing argument")
	}
}
