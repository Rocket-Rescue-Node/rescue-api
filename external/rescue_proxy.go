package external

import (
	"context"
	"crypto/tls"
	"errors"
	"time"

	proxy "github.com/Rocket-Rescue-Node/rescue-proxy/pb"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type RescueProxyAPIClient struct {
	address string
	secure  bool
	logger  *zap.Logger

	conn   *grpc.ClientConn
	client proxy.ApiClient
}

func NewRescueProxyAPIClient(logger *zap.Logger, address string, secure bool) *RescueProxyAPIClient {
	return &RescueProxyAPIClient{
		address: address,
		secure:  secure,
		logger:  logger,
	}
}

func (c *RescueProxyAPIClient) connect() error {
	var err error

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Try to connect to the Rescue Proxy API using TLS.
	// An empty TLS config will use the system's root CAs.
	tc := credentials.NewTLS(&tls.Config{})
	if c.conn, err = grpc.DialContext(ctx,
		c.address,
		grpc.WithTransportCredentials(tc),
		grpc.WithBlock()); err == nil {

		c.client = proxy.NewApiClient(c.conn)
		c.logger.Debug("connected to rescue-proxy with TLS", zap.String("address", c.address))
		return nil
	}

	// If TLS fails, try falling back to insecure gRPC.
	if c.secure {
		c.logger.Debug("not attempting to connect to rescue-proxy without TLS, since insecure grpc is disallowed", zap.String("address", c.address))
		return err
	}

	c.logger.Debug("attempting to connect to rescue-proxy without TLS, since insecure grpc is allowed", zap.String("address", c.address))

	ctx, cancel2 := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel2()

	if c.conn, err = grpc.DialContext(ctx,
		c.address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock()); err != nil {

		return err
	}

	c.logger.Debug("connected to rescue-proxy without TLS", zap.String("address", c.address))

	c.client = proxy.NewApiClient(c.conn)
	return nil
}

func (c *RescueProxyAPIClient) ensureConnection() error {
	if c.conn == nil || c.client == nil {
		c.logger.Debug("not yet connected - connecting", zap.String("address", c.address))
		if err := c.connect(); err != nil {
			return err
		}
	}

	return nil
}

func (c *RescueProxyAPIClient) GetRocketPoolNodes() ([][]byte, error) {
	// Connect if not yet connected.
	if err := c.ensureConnection(); err != nil {
		return nil, err
	}
	c.logger.Debug("requesting rp nodes")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := c.client.GetRocketPoolNodes(ctx, &proxy.RocketPoolNodesRequest{})
	if err != nil {
		return nil, err
	}
	return r.GetNodeIds(), nil
}

func (c *RescueProxyAPIClient) GetWithdrawalAddresses() ([][]byte, error) {
	// Connect if not yet connected.
	if err := c.ensureConnection(); err != nil {
		return nil, err
	}
	c.logger.Debug("requesting solo validator withdrawal addresses")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := c.client.GetSoloValidators(ctx, &proxy.SoloValidatorsRequest{})
	if err != nil {
		return nil, err
	}
	return r.GetWithdrawalAddresses(), nil
}

func (c *RescueProxyAPIClient) ValidateEIP1271(dataHash *common.Hash, signature *[]byte, address *common.Address) (bool, error) {
	// Connect if not yet connected.
	if err := c.ensureConnection(); err != nil {
		return false, err
	}
	c.logger.Debug("requesting eip1271 validation")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := c.client.ValidateEIP1271(ctx, &proxy.ValidateEIP1271Request{
		DataHash:  dataHash.Bytes(),
		Signature: *signature,
		Address:   address.Bytes(),
	})
	if err != nil {
		return false, err
	}
	rErr := r.GetError()
	if rErr != "" {
		return false, errors.New(rErr)
	}
	return r.GetValid(), nil
}

func (c *RescueProxyAPIClient) Close() error {
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	c.client = nil
	return err
}
