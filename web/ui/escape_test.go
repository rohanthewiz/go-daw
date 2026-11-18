package ui

import (
	"strings"
	"testing"

	"github.com/rohanthewiz/element"
	"github.com/rohanthewiz/go-daw/store"
)

// TestSceneNamesAreEscaped: a quote in a scene name used to cut its
// <option value> short (Recall/Delete then posted a name that does not
// exist), and markup in a name was injected into every page load.
func TestSceneNamesAreEscaped(t *testing.T) {
	b := element.NewBuilder()
	TransportBar{Scenes: []store.SceneInfo{{Name: `Bob's "Live" <b>mix</b>`}}}.Render(b)
	html := b.String()

	if strings.Contains(html, "<b>mix</b>") {
		t.Fatal("scene name markup reached the page unescaped")
	}
	want := `value="Bob&#39;s &#34;Live&#34; &lt;b&gt;mix&lt;/b&gt;"`
	if !strings.Contains(html, want) {
		t.Fatalf("escaped option value not found; want %s in:\n%s", want, html)
	}
}
