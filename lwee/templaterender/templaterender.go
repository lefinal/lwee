// Package templaterender allows rendering templates using Renderer.
package templaterender

import (
	"bytes"
	"fmt"
	"github.com/lefinal/meh"
	"html/template"
)

// Data holds flow and action data and is used as data when rendering templates
// with Renderer.
type Data struct {
	Flow   FlowData
	Action ActionData
}

// FlowData for usage in Data.Flow.
type FlowData struct {
	Raw map[string]any
}

// ActionData for usage in Data.Action.
type ActionData struct {
	Name   string
	Extras any
}

// Renderer allows rendering templates. Create one with New and use methods like
// Renderer.RenderString.
type Renderer struct {
	data Data
}

// New creates a new Renderer.
func New(data Data) *Renderer {
	return &Renderer{
		data: data,
	}
}

func (filler *Renderer) parse(str string) (*template.Template, error) {
	fillTemplate, err := template.New("").Option("missingkey=error").Parse(str)
	if err != nil {
		return nil, meh.NewBadInputErrFromErr(err, "parse", meh.Details{"str": str})
	}
	return fillTemplate, nil
}

func (filler *Renderer) render(str *string) error {
	if str == nil {
		return nil
	}
	fillTemplate, err := filler.parse(*str)
	if err != nil {
		return meh.Wrap(err, "new template", nil)
	}
	var b bytes.Buffer
	err = fillTemplate.Execute(&b, filler.data)
	if err != nil {
		return meh.Wrap(err, "execute template", meh.Details{
			"template":      *str,
			"template_data": fmt.Sprintf("%+v", filler.data),
		})
	}
	*str = b.String()
	return nil
}

// RenderString renders the provided string template using the Renderer's data.
// It modifies the input string pointer in-place. If rendering fails, an error is
// returned.
func (filler *Renderer) RenderString(str *string) error {
	err := filler.render(str)
	if err != nil {
		return meh.Wrap(err, "render", meh.Details{"render_value": str})
	}
	return nil
}

// RenderStrings renders the provided string list using the Renderer's data. It
// modifies the input list in-place, replacing each element with its rendered
// value. If rendering fails for any element, an error is returned. Example
// usage:
//
//	err := renderer.RenderStrings(runner.Command)
//	if err != nil {
//	  return meh.Wrap(err, "render command", nil)
//	}
func (filler *Renderer) RenderStrings(strList []string) error {
	for i, str := range strList {
		str := str
		err := filler.render(&str)
		if err != nil {
			return meh.Wrap(err, "render", meh.Details{
				"elements":             strList,
				"render_element_index": i,
				"render_element_value": str,
			})
		}
		strList[i] = str
	}
	return nil
}

// RenderStringStringMap renders the values in the provided string map using the
// Renderer's data. It modifies the values of the input map in-place. If
// rendering fails for any value, an error is returned.
func (filler *Renderer) RenderStringStringMap(m map[string]string) error {
	for k, v := range m {
		str := v
		err := filler.render(&str)
		if err != nil {
			return meh.Wrap(err, "render", meh.Details{
				"render_element_key":   k,
				"render_element_value": v,
			})
		}
		m[k] = str
	}
	return nil
}
