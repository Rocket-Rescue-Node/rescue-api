package external

import (
	"encoding/json"
	"net/url"

	"github.com/Rocket-Pool-Rescue-Node/rescue-api/util"
)

const (
	rocketscanNodesPath = "/nodes/list/"
)

type RocketscanAPIClient struct {
	url string
}

type RocketscanAPINode struct {
	Address string `json:"address"`
}

func NewRocketscanAPIClient(url string) *RocketscanAPIClient {
	return &RocketscanAPIClient{url: url}
}

func (c *RocketscanAPIClient) GetRocketPoolNodes() ([]RocketscanAPINode, error) {
	url, err := url.JoinPath(c.url, rocketscanNodesPath)
	if err != nil {
		return nil, err
	}

	// Limit the response size to 10MB (average response size is 3MB).
	body, err := util.HTTPLimitedGet(url, 10*1024*1024)
	if err != nil {
		return nil, err
	}

	nodes := make([]RocketscanAPINode, 0, 256)
	if err := json.Unmarshal(body, &nodes); err != nil {
		return nil, err
	}
	return nodes, nil
}

func (c *RocketscanAPIClient) Close() error {
	return nil
}
