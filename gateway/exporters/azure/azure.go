package azure

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"net/http"

	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	"github.com/openconfig/gnmi-gateway/gateway/exporters"
	"github.com/openconfig/gnmi-gateway/gateway/utils"
	"github.com/openconfig/gnmi/cache"
	"github.com/openconfig/gnmi/ctree"
	gnmipb "github.com/openconfig/gnmi/proto/gnmi"
)

const Name = "azure"

var _ exporters.Exporter = new(AzureExporter)

type Point struct {
	Tags        map[string]string
	Measurement string
	Fields      map[string]interface{}
	Timestamp   time.Time
}

func init() {
	exporters.Register(Name, NewAzureExporter)
}

func NewAzureExporter(config *configuration.GatewayConfig) exporters.Exporter {

	return &AzureExporter{
		config: config,
	}
}

type AzureToken struct {
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	ExtExpiresIn int    `json:"ext_expires_in"`
	ExpiresOn    int    `json:"expires_on"`
	NotBefore    int    `json:"not_before"`
	Resource     string `json:"resource"`
	Token        string `json:"access_token"`
}

type AzureExporter struct {
	cache          *cache.Cache
	config         *configuration.GatewayConfig
	tokenEndpoint  string
	metricEndpoint string
	token          *AzureToken
}

func (e *AzureExporter) Name() string {
	return Name
}

func (e *AzureExporter) Export(leaf *ctree.Leaf) {
	notification := leaf.Value().(*gnmipb.Notification)
	e.config.Log.Info().Msg(utils.GNMINotificationPrettyString(notification))

	point := Point{
		Timestamp: time.Unix(0, notification.Timestamp),
		Tags:      make(map[string]string),
		Fields:    make(map[string]interface{}),
	}

	for _, update := range notification.Update {
		var name string

		value, isNumber := utils.GetNumberValues(update.Val)

		if !isNumber {
			continue
		}

		elems, keys, err := extractPrefixAndPathKeys(notification.GetPrefix(), update.GetPath())
		if err != nil {
			e.config.Log.Info().Msg(fmt.Sprintf("Failed to extract path or keys: %s", err))
		}

		name, elems = elems[len(elems)-1], elems[:len(elems)-1]

		if point.Measurement == "" {
			path := strings.Join(elems, "/")

			p := notification.GetPrefix()
			target := p.GetTarget()
			origin := p.GetOrigin()

			keys["path"] = path
			if origin != "" {
				keys["origin"] = origin
			}

			if target != "" {
				keys["target"] = target
			}

			point.Measurement = pathToMetricName(path)
			point.Tags = keys
		}

		point.Fields[pathToMetricName(name)] = value
	}

	if point.Measurement == "" {
		e.config.Log.Error().Msg("Point measurement is empty. Returning.")
		return
	}

	pointJSON, err := json.Marshal(point)

	if err != nil {
		e.config.Log.Error().Msg("Failed to marshal point into JSON")
		return
	}

	fmt.Println("\n", string(pointJSON))

	if err = writeJSONMetric(string(pointJSON), e.token.Token, e.metricEndpoint); err != nil {
		e.config.Log.Error().Msg("Failed to push metric to Azure Monitor endpoint")
		return
	}
}

func (e *AzureExporter) Start(cache *cache.Cache) error {
	_ = cache
	e.config.Log.Info().Msg("Starting Azure Monitor exporter.")

	if e.config.Exporters.AzureMetricEndpoint == "" {
		return errors.New("AzureMetricEndpoint is not set")
	}

	if e.config.Exporters.AzureTokenEndpoint == "" {
		return errors.New("AzureTokenEndpoint is not set")
	}

	if e.config.Exporters.AzureClientID == "" {
		return errors.New("AzureClientID is not set")
	}

	if e.config.Exporters.AzureClientSecret == "" {
		return errors.New("AzureClientSecret is not set")
	}

	e.metricEndpoint = e.config.Exporters.AzureMetricEndpoint
	e.tokenEndpoint = e.config.Exporters.AzureTokenEndpoint

	var err error
	e.token, err = generateToken(e.tokenEndpoint, e.config.Exporters.AzureClientID, e.config.Exporters.AzureClientSecret)

	if err != nil {
		return err
	}

	return nil
}

func pathToMetricName(path string) string {
	return strings.ReplaceAll(strings.ReplaceAll(path, "/", "_"), "-", "_")
}

func extractPrefixAndPathKeys(prefix *gnmipb.Path, path *gnmipb.Path) ([]string, map[string]string, error) {
	elems := make([]string, 0)
	keys := make(map[string]string)
	for _, path := range []*gnmipb.Path{prefix, path} {
		for _, e := range path.Elem {
			name := e.Name
			elems = append(elems, name)

			for k, v := range e.GetKey() {
				if _, ok := keys[k]; ok {
					keys[name+"/"+k] = v
				} else {
					keys[k] = v
				}
			}
		}
	}

	if len(elems) == 0 {
		return elems, keys, errors.New("Path contains no elems")
	}

	return elems, keys, nil
}

func generateToken(endpoint string, clientID string, clientSecret string) (*AzureToken, error) {
	fmt.Println("\nGenerating token using:")
	fmt.Println("\nAuth endpoint: ", endpoint)
	fmt.Println("\nAuth clientID: ", clientID)
	fmt.Println("\nAuth clientSecret: ", clientSecret)
	fmt.Println("")
	response, err := http.PostForm(
		endpoint,
		url.Values{
			"grant_type":    {"client_credentials"},
			"client_id":     {clientID},
			"client_secret": {clientSecret},
			"resource":      {"https://monitoring.azure.com/"},
		},
	)
	if err != nil {
		fmt.Println("Error generating token")
		return nil, err
	}
	responseBody, err := io.ReadAll(response.Body)
	fmt.Println("\nGot response body: ", string(responseBody))

	var token AzureToken
	json.Unmarshal(responseBody, &token)
	return &token, nil
}

func writeJSONMetric(data string, token string, metricEndpoint string) error {
	return nil
}
