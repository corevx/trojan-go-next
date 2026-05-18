package rest

const Name = "REST_SERVICE"

type RESTConfig struct {
	Enabled  bool     `json:"enabled" yaml:"enabled"`
	RESTPort int      `json:"rest_port" yaml:"rest-port"`
	APIKey   string   `json:"api_key" yaml:"api-key"`
	CORS     []string `json:"cors" yaml:"cors"`
}
