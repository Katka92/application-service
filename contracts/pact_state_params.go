package contracts

// This should correspond with the https://github.com/openshift/hac-dev/blob/main/pact-tests/states/state-params.ts

type Comp struct {
	app  AppParams
	repo string
	name string
}

type AppParams struct {
	appName   string
	namespace string
}

func parseApp(params map[string]interface{}) AppParams {
	return AppParams{
		params["params"].(map[string]interface{})["appName"].(string),
		params["params"].(map[string]interface{})["namespace"].(string),
	}
}

func parseComp(params map[string]interface{}) []Comp {
	tmp := params["params"].(map[string]interface{})["components"].([]interface{})
	var components []Comp
	for _, compToParse := range tmp {
		component := compToParse.(map[string]interface{})
		appParsed := AppParams{component["app"].(map[string]interface{})["appName"].(string),
			component["app"].(map[string]interface{})["namespace"].(string)}
		compParsed := Comp{appParsed, component["repo"].(string), component["compName"].(string)}
		components = append(components, compParsed)
	}
	return components
}
