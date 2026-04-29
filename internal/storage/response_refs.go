// Copyright 2026 ICAP Mock

package storage

import "fmt"

func resolveScenarioResponseRefs(scenarios []Scenario, templates map[string]ResponseTemplate) error {
	if len(templates) == 0 {
		return nil
	}
	for i := range scenarios {
		resolved, err := resolveResponseRef(scenarios[i].Response, templates)
		if err != nil {
			return fmt.Errorf("scenario %q response: %w", scenarios[i].Name, err)
		}
		scenarios[i].Response = resolved
	}
	return nil
}

func resolveResponseRef(resp ResponseTemplate, templates map[string]ResponseTemplate) (ResponseTemplate, error) {
	if resp.Use == "" {
		return resp, nil
	}
	base, ok := templates[resp.Use]
	if !ok {
		return ResponseTemplate{}, fmt.Errorf("use %q: response is not defined", resp.Use)
	}
	if base.Use != "" {
		return ResponseTemplate{}, fmt.Errorf("response %q cannot reference another response", resp.Use)
	}
	return mergeResponseTemplate(base, resp), nil
}

func mergeResponseTemplate(base, over ResponseTemplate) ResponseTemplate {
	out := base
	out.Use = ""
	mergeResponseScalars(&out, over)
	if len(over.Headers) > 0 {
		out.Headers = mergeHeaders(base.Headers, over.Headers)
	}
	if len(over.HTTPHeaders) > 0 {
		out.HTTPHeaders = mergeHeaders(base.HTTPHeaders, over.HTTPHeaders)
	}
	if over.Stream != nil {
		out.Stream = over.Stream
	}
	return out
}

func mergeResponseScalars(out *ResponseTemplate, over ResponseTemplate) {
	if over.Body != "" {
		out.Body = over.Body
	}
	if over.BodyFile != "" {
		out.BodyFile = over.BodyFile
	}
	if over.HTTPBody != "" {
		out.HTTPBody = over.HTTPBody
	}
	if over.HTTPBodyFile != "" {
		out.HTTPBodyFile = over.HTTPBodyFile
	}
	if over.ICAPStatus != 0 {
		out.ICAPStatus = over.ICAPStatus
	}
	if over.Status != 0 {
		out.Status = over.Status
		out.ICAPStatus = over.Status
	}
	if over.HTTPStatus != 0 {
		out.HTTPStatus = over.HTTPStatus
	}
	if over.Delay != 0 {
		out.Delay = over.Delay
	}
}
