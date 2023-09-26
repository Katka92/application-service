package contracts

// The list of used states and their params can be found there: https://github.com/openshift/hac-dev/blob/main/pact-tests/states/states.ts#L18

import (
	models "github.com/pact-foundation/pact-go/v2/models"
)

func setupStateHandler() models.StateHandlers {
	return models.StateHandlers{
		"Application doesn't exist":  appDoesntExist,
		"Application exists":         createApp,
		"Application has components": createComponents,
	}
}
