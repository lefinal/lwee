package templaterender

import (
	"bytes"
	"fmt"
	"github.com/lefinal/meh"
	"html/template"
)

type Data struct {
	Flow   FlowData
	Action ActionData
}

type FlowData struct {
	Raw map[string]any
}

type ActionData struct {
	Name   string
	Extras any
}

type Renderer struct {
	data Data
}

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

func (filler *Renderer) RenderString(str *string) error {
	err := filler.render(str)
	if err != nil {
		return meh.Wrap(err, "render", meh.Details{"render_value": str})
	}
	return nil
}

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
