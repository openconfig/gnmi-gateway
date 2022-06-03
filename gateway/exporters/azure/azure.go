package azure

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"net/http"

	"path"

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
	ExpiresIn    string `json:"expires_in"`
	ExtExpiresIn string `json:"ext_expires_in"`
	ExpiresOn    string `json:"expires_on"`
	NotBefore    string `json:"not_before"`
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

	timestamp := time.Unix(0, notification.Timestamp)
	beforeLimit := (time.Now()).Add(-30 * time.Minute)
	afterLimit := (time.Now()).Add(4 * time.Minute)

	if timestamp.Before(beforeLimit) || timestamp.After(afterLimit) {
		return
	}

	metric := Metric{
		Timestamp: timestamp,
	}

	var clientID string
	var clientSecret string
	var tenantID string

	var deviceName string
	var rackName string
	var fabricName string

	var metricEndpoint string
	var tokenEndpoint string
	foundNumeric := false

	for _, update := range notification.Update {
		var name string

		value, isNumber := utils.GetNumberValues(update.Val)

		if !isNumber {
			continue
		}

		foundNumeric = true

		elems, keys, err := extractPrefixAndPathKeys(notification.GetPrefix(), update.GetPath())
		if err != nil {
			e.config.Log.Info().Msg(fmt.Sprintf("Failed to extract path or keys: %s", err))
		}

		name, elems = elems[len(elems)-1], elems[:len(elems)-1]

		if baseData.Measurement == "" {
			measurementPath := strings.Join(elems, "/")

			p := notification.GetPrefix()
			target := p.GetTarget()
			origin := p.GetOrigin()

			keys["path"] = measurementPath
			if origin != "" {
				keys["origin"] = origin
			}

			if target != "" {
				keys["target"] = target
			}

			baseData.Measurement = pathToMetricName(measurementPath)

			point.Tags = keys

			targetConfig, found := (*e.connMgr).GetTargetConfig(point.Tags["target"])

			if found {

				deviceID, exists := targetConfig.Meta["deviceID"]
				if exists {
					point.Tags["deviceID"] = deviceID
					deviceName = path.Base(deviceID)
				} else {
					e.config.Log.Error().Msg("Device ARM ID is not set in the metadata of device" + point.Tags["target"])
					return
				}

				rackID, exists := targetConfig.Meta["rackID"]
				if exists {
					point.Tags["rackID"] = rackID
					rackName = path.Base(rackID)
				} else {
					e.config.Log.Error().Msg("Rack ARM ID is not set in the metadata of device" + point.Tags["target"])
					return
				}

				fabricID, exists := targetConfig.Meta["fabricID"]
				if exists {
					point.Tags["fabricID"] = fabricID
					fabricName = path.Base(fabricID)
				} else {
					e.config.Log.Error().Msg("Fabric ARM ID is not set in the metadata of device" + point.Tags["target"])
					return
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

				// Push metrics to fabric
				metricEndpoint = "https://" + location + ".monitoring.azure.com" + fabricID + "/metrics"
			} else {
				e.config.Log.Error().Msg("Target config not found for target: " + point.Tags["target"])
				return
			}
		}

		point.Fields[pathToMetricName(name)] = value
	}

	if !foundNumeric {
		e.config.Log.Debug().Msg("No numeric values found in notification updates. Skipping...")
		return
	}

	if baseData.Measurement == "" {
		e.config.Log.Info().Msg("Point measurement is empty. Returning.")
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

	dimNames = append(dimNames, []string{"fabricName", "rackName", "deviceName"}...)
	dimValues = append(dimValues, []string{fabricName, rackName, deviceName}...)

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

	// e.config.Log.Info().Msg(string(metricJSON))

	if e.token == nil {
		if tokenEndpoint == "" {
			e.config.Log.Error().Msg("Token endpoint is not set: " + tokenEndpoint)
			return
		}

		e.token, err = generateToken(tokenEndpoint, clientID, clientSecret, e.config.Log)

		if err != nil {
			e.config.Log.Error().Msg("Error generating token: " + err.Error())
			return
		}
	} else {
		tokenExpireUnix, err := strconv.Atoi(e.token.ExpiresOn)
		if err != nil {
			return
		}

		tokenExpireDate := time.Unix(int64(tokenExpireUnix), 0).Add((-1) * time.Minute)

		if time.Now().After(tokenExpireDate) {
			e.token, err = generateToken(tokenEndpoint, clientID, clientSecret, e.config.Log)

			if err != nil {
				e.config.Log.Error().Msg("Error generating token: " + err.Error())
				return
			}
		}
	}

	if err != nil {
		e.config.Log.Error().Msg(err.Error())
		return
	}
	if err = writeJSONMetric(metricJSON, e.token.Token, metricEndpoint, e.config.Log); err != nil {
		e.config.Log.Error().Msg("Failed to push metric to Azure Monitor endpoint: \n" + err.Error())
		if err.Error() == "401" {
			e.token, err = generateToken(tokenEndpoint, clientID, clientSecret, e.config.Log)
			if err != nil {
				e.config.Log.Error().Msg("Error generating token: " + err.Error())
				return
			}
			err = writeJSONMetric(metricJSON, e.token.Token, metricEndpoint, e.config.Log)
			if err != nil {
				return
			}
		} else {
			return
		}
	}
}

func (e *AzureExporter) Start(connMgr *connections.ConnectionManager) error {
	e.connMgr = connMgr
	e.config.Log.Info().Msg("Starting Azure Monitor exporter.")

	return nil
}

func pathToMetricName(metricPath string) string {
	return strings.ReplaceAll(strings.ReplaceAll(metricPath, "/", "_"), "-", "_")
}

func extractPrefixAndPathKeys(prefix *gnmipb.Path, metricPath *gnmipb.Path) ([]string, map[string]string, error) {
	elems := make([]string, 0)
	keys := make(map[string]string)
	for _, metricPath := range []*gnmipb.Path{prefix, metricPath} {
		for _, e := range metricPath.Elem {
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
	if endpoint == "" {
		return nil, errors.New("generateToken: endpoint is not set")
	}
	if clientID == "" {
		return nil, errors.New("generateToken: clientID is not set")
	}
	if clientSecret == "" {
		return nil, errors.New("generateToken: endpoint is not set")
	}

	responseURL := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"resource":      {"https://monitoring.azure.com/"},
	}
	response, err := http.PostForm(
		endpoint,
		responseURL,
	)
	if err != nil {
		logger.Error().Msg("Error generating token")
		logger.Error().Msg("Token endpoint: " + endpoint)
		logger.Error().Msg("URL values: { client_id: " + clientID)
		return nil, err
	}
	responseBody, err := ioutil.ReadAll(response.Body)

	var token AzureToken
	json.Unmarshal(responseBody, &token)

	tokenExpireUnix, err := strconv.Atoi(token.ExpiresOn)
	if err != nil {
		return nil, err
	}

	tokenExpireDate := time.Unix(int64(tokenExpireUnix), 0)
	logger.Info().Msg(fmt.Sprintf("New Azure Monitor token is valid until: %s", tokenExpireDate))

	return &token, nil
}

func writeJSONMetric(jsonData []byte, token string, metricEndpoint string, logger zerolog.Logger) error {

	_, err := url.ParseRequestURI(metricEndpoint)

	if err != nil {
		logger.Error().Msg("Invalid URL: " + metricEndpoint)
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
		respBody, err := ioutil.ReadAll(response.Body)
		if err != nil {
			return err
		}
		logger.Error().Msg("\nMetric publishing failed with message:\n" + string(respBody))

		return errors.New(strconv.Itoa(response.StatusCode))
	}
	return nil
}
