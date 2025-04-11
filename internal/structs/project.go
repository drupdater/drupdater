package structs

type EnvironmentVariable struct {
	Name  string `dynamodbav:"Name"`
	Value string `dynamodbav:"Value"`
}

type UpdateStrategy struct {
	AutoMerge bool   `dynamodbav:"AutoMerge"`
	Branch    string `dynamodbav:"Branch"`
}

type UpdateStrategies struct {
	Regular  UpdateStrategy `dynamodbav:"Regular"`
	Security UpdateStrategy `dynamodbav:"Security"`
}

type Project struct {
	OrganisationID       string                `dynamodbav:"PK"`
	ProjectID            string                `dynamodbav:"SK"`
	CreatedAt            string                `dynamodbav:"CreatedAt"`
	EntityType           string                `dynamodbav:"EntityType"`
	EnvironmentVariables []EnvironmentVariable `dynamodbav:"EnvironmentVariables"`
	Name                 string                `dynamodbav:"Name"`
	PhpVersion           string                `dynamodbav:"PhpVersion"`
	Sites                []string              `dynamodbav:"Sites"`
	UpdateStrategies     UpdateStrategies      `dynamodbav:"UpdateStrategies"`
	URL                  string                `dynamodbav:"URL"`
}

type ProjectAccessToken struct {
	ProjectID      string `dynamodbav:"PK"`
	EncryptedToken string `dynamodbav:"Token"`
	ExpireAt       string `dynamodbav:"ExpireAt"`
}
