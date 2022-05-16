package azure

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"net/http"

	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	"github.com/openconfig/gnmi-gateway/gateway/connections"
	"github.com/openconfig/gnmi-gateway/gateway/exporters"
	"github.com/openconfig/gnmi-gateway/gateway/utils"
	"github.com/openconfig/gnmi/ctree"
	gnmipb "github.com/openconfig/gnmi/proto/gnmi"
	"github.com/rs/zerolog"
)

const Name = "azure"

var _ exporters.Exporter = new(AzureExporter)

type Metric struct {
	Timestamp  time.Time `json:"time"`
	MetricData Data      `json:"data"`
}
type Data struct {
	Base BaseData `json:"baseData"`
}
type BaseData struct {
	Measurement string                   `json:"metric"`
	Namespace   string                   `json:"namespace"`
	DimNames    []string                 `json:"dimNames"`
	Series      []map[string]interface{} `json:"series"`
}

type Point struct {
	Tags   map[string]string
	Fields map[string]interface{}
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
	config  *configuration.GatewayConfig
	connMgr *connections.ConnectionManager
	token   *AzureToken
}

func (e *AzureExporter) Name() string {
	return Name
}

func (e *AzureExporter) Export(leaf *ctree.Leaf) {
	notification := leaf.Value().(*gnmipb.Notification)

	point := Point{
		Fields: make(map[string]interface{}),
	}

	baseData := BaseData{}

	metric := Metric{
		Timestamp: time.Unix(0, notification.Timestamp),
	}

	var clientID string
	var clientSecret string
	var tenantID string
	var metricEndpoint string
	var tokenEndpoint string

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

		if baseData.Measurement == "" {
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

			baseData.Measurement = pathToMetricName(path)

			point.Tags = keys
			targetConfig, found := (*e.connMgr).GetTargetConfig(point.Tags["target"])
			if found {
				deviceID, exists := targetConfig.Meta["deviceID"]
				if exists {
					point.Tags["deviceID"] = deviceID
				}
				rackID, exists := targetConfig.Meta["rackID"]
				if exists {
					point.Tags["rackID"] = rackID
				}
				fabricID, exists := targetConfig.Meta["fabricID"]
				if exists {
					point.Tags["fabricID"] = fabricID
				}
				tenantID, exists = targetConfig.Meta["tenantID"]
				if !exists {
					e.config.Log.Error().Msg("Azure Tenant ID is not set in the metadata of device" + point.Tags["target"])
					return
				}
				tokenEndpoint = "https://login.microsoftonline.com/" + tenantID + "/oauth2/token"
				clientID, exists = targetConfig.Meta["clientID"]
				if !exists {
					e.config.Log.Error().Msg("Azure Client ID is not set in the metadata of device" + point.Tags["target"])
					return
				}
				clientSecret, exists = targetConfig.Meta["clientSecret"]
				if !exists {
					e.config.Log.Error().Msg("Azure Client Secret is not set in the metadata of device" + point.Tags["target"])
					return
				}
				location, exists := targetConfig.Meta["location"]
				if !exists {
					e.config.Log.Error().Msg("Azure Resource Group location is not set in the metadata of device" + point.Tags["target"])
					return
				}
				metricEndpoint = "https://" + location + ".monitoring.azure.com" + deviceID + "/metrics"
			}
		}

		point.Fields[pathToMetricName(name)] = value
	}

	if baseData.Measurement == "" {
		//e.config.Log.Error().Msg("Point measurement is empty. Returning.")
		return
	}

	dimNames := []string{"fabricID", "rackID", "deviceID"}
	tagKeys := make([]string, 0, len(point.Tags))
	tagVals := make([]string, 0, len(point.Tags))
	for k, v := range point.Tags {
		tagKeys = append(tagKeys, k)
		tagVals = append(tagVals, v)
	}
	dimNames = append(baseData.DimNames, tagKeys...)

	dimValues := []string{}
	dimValues = append(dimValues, tagVals...)
	baseData.DimNames = dimNames

	value := make(map[string]interface{})

	value["dimValues"] = dimValues

	for k, v := range point.Fields {
		value[k] = v
		switch v.(type) {
		case int, float64, float32:
			value["min"] = v
			value["max"] = v
			value["sum"] = v
		default:
			e.config.Log.Error().Msg("Received metric field with non-numeric type")
			return
		}

	}
	value["count"] = 1

	baseData.Series = append(baseData.Series, value)

	baseData.Namespace = "Interface metrics"

	metric.MetricData.Base = baseData

	metricJSON, err := json.Marshal(metric)

	if err != nil {
		e.config.Log.Error().Msg("Failed to marshal point into JSON")
		return
	}

	e.config.Log.Debug().Msg(string(metricJSON))

	// TODO - Take token validity into consideration
	if e.token == nil {
		e.token, err = generateToken(tokenEndpoint, clientID, clientSecret, e.config.Log)
	}

	if err != nil {
		e.config.Log.Error().Msg(err.Error())
		return
	}
	if err = writeJSONMetric(metricJSON, e.token.Token, metricEndpoint, e.config.Log); err != nil {
		e.config.Log.Error().Msg("Failed to push metric to Azure Monitor endpoint: \n" + err.Error())
		return
	}
}

func (e *AzureExporter) Start(connMgr *connections.ConnectionManager) error {
	e.connMgr = connMgr
	e.config.Log.Info().Msg("Starting Azure Monitor exporter.")

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

func generateToken(endpoint string, clientID string, clientSecret string, logger zerolog.Logger) (*AzureToken, error) {
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
		logger.Debug().Msg("Error generating token")
		return nil, err
	}
	responseBody, err := io.ReadAll(response.Body)

	var token AzureToken
	json.Unmarshal(responseBody, &token)
	return &token, nil
}

func writeJSONMetric(jsonData []byte, token string, metricEndpoint string, logger zerolog.Logger) error {

	_, err := url.ParseRequestURI(metricEndpoint)

	if err != nil {
		return err
	}

	var m Metric
	if err = json.Unmarshal(jsonData, &m); err != nil {
		return err
	}

	bearer := "Bearer " + token
	request, err := http.NewRequest("POST", metricEndpoint, bytes.NewBuffer(jsonData))

	if err != nil {
		return err
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", bearer)

	client := &http.Client{}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.Status != "200 OK" {
		logger.Error().Msg("response status:" + response.Status)
		respBody, err := io.ReadAll(response.Body)
		if err != nil {
			return err
		}
		logger.Error().Msg("\nMetric publishing failed with message:\n" + string(respBody))
		return nil
	}
	return nil
}
