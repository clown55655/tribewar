package server

import (
	"os"
	"strconv"
	"strings"

	"github.com/mitchellh/mapstructure"
	"github.com/spf13/viper"

	"tribeway/internal/database"
	"tribeway/internal/discovery"
	"tribeway/internal/logger"
	"tribeway/internal/mq"
)

// ServerConfig 服务器配置
type ServerConfig struct {
	Server struct {
		Name    string `yaml:"name"`
		Version string `yaml:"version"`
		Debug   bool   `yaml:"debug"`
	} `yaml:"server"`

	Network struct {
		TCPPort             int    `yaml:"tcp_port"`
		RPCPort             int    `yaml:"rpc_port"`
		HTTPPort            int    `yaml:"http_port"`
		MaxConnections      int    `yaml:"max_connections"`
		ReadTimeout         int    `yaml:"read_timeout"`
		WriteTimeout        int    `yaml:"write_timeout"`
		MaxPacketSize       int    `yaml:"max_packet_size"`
		AdvertiseAddress    string `yaml:"advertise_address"`
		AdvertiseAddressEnv string `yaml:"advertise_address_env"`
	} `yaml:"network"`

	Database struct {
		Redis   database.RedisConfig `yaml:"redis"`
		MongoDB database.MongoConfig `yaml:"mongodb"`
	} `yaml:"database"`

	NSQ mq.NSQConfig `yaml:"nsq"`

	ETCD discovery.ETCDConfig `yaml:"etcd"`

	Log logger.LogConfig `yaml:"log"`

	Nodes map[string]struct {
		Count int   `yaml:"count"`
		Ports []int `yaml:"ports"`
	} `yaml:"nodes"`

	ObjectPool struct {
		MessagePoolSize    int `yaml:"message_pool_size"`
		ConnectionPoolSize int `yaml:"connection_pool_size"`
		ActorPoolSize      int `yaml:"actor_pool_size"`
	} `yaml:"object_pool"`

	RPC struct {
		PoolSize    int `yaml:"pool_size"`
		MaxIdle     int `yaml:"max_idle"`
		IdleTimeout int `yaml:"idle_timeout"`
	} `yaml:"rpc"`

	Security struct {
		Auth struct {
			PasswordHashAlgorithm string `yaml:"password_hash_algorithm"`
			TokenSecret           string `yaml:"token_secret"`
			TokenSecretEnv        string `yaml:"token_secret_env"`
			TokenExpiryHours      int    `yaml:"token_expiry_hours"`
		} `yaml:"auth"`
		Secrets struct {
			EncryptionKeyEnv string `yaml:"encryption_key_env"`
			JWTSecretEnv     string `yaml:"jwt_secret_env"`
		} `yaml:"secrets"`
		Monitoring struct {
			BindAddress            string   `yaml:"bind_address"`
			AdminTokenEnv          string   `yaml:"admin_token_env"`
			AllowedCIDRs           []string `yaml:"allowed_cidrs"`
			ProtectMetricsEndpoint bool     `yaml:"protect_metrics_endpoint"`
		} `yaml:"monitoring"`
		GM struct {
			AdminUserIDs    []uint64 `yaml:"admin_user_ids"`
			AdminUserIDsEnv string   `yaml:"admin_user_ids_env"`
		} `yaml:"gm"`
	} `yaml:"security"`
}

func loadConfig(configFile string) (*ServerConfig, error) {
	viper.SetConfigFile(configFile)
	viper.SetConfigType("yaml")

	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}

	var config ServerConfig
	if err := viper.Unmarshal(
		&config,
		func(dc *mapstructure.DecoderConfig) {
			dc.TagName = "yaml"
		},
		viper.DecodeHook(mapstructure.StringToTimeDurationHookFunc()),
	); err != nil {
		return nil, err
	}

	applyEnvOverrides(&config)
	return &config, nil
}

func applyEnvOverrides(config *ServerConfig) {
	if value := os.Getenv("TRIBEWAY_NETWORK_TCP_PORT"); value != "" {
		if port, err := strconv.Atoi(value); err == nil {
			config.Network.TCPPort = port
		}
	}
	if value := os.Getenv("TRIBEWAY_NETWORK_RPC_PORT"); value != "" {
		if port, err := strconv.Atoi(value); err == nil {
			config.Network.RPCPort = port
		}
	}
	if value := os.Getenv("TRIBEWAY_NETWORK_HTTP_PORT"); value != "" {
		if port, err := strconv.Atoi(value); err == nil {
			config.Network.HTTPPort = port
		}
	}
	if value := os.Getenv("TRIBEWAY_REDIS_ADDR"); value != "" {
		config.Database.Redis.Addr = value
	}
	if value := os.Getenv("TRIBEWAY_MONGODB_URI"); value != "" {
		config.Database.MongoDB.URI = value
	}
	if value := os.Getenv("TRIBEWAY_MONGODB_USERNAME"); value != "" {
		config.Database.MongoDB.Username = value
	}
	if value := os.Getenv("TRIBEWAY_MONGODB_AUTH_SOURCE"); value != "" {
		config.Database.MongoDB.AuthSource = value
	}
	if value := os.Getenv("TRIBEWAY_MONGODB_PASSWORD_ENV"); value != "" {
		config.Database.MongoDB.PasswordEnv = value
	}
	if value := os.Getenv("TRIBEWAY_NSQD_ADDRESS"); value != "" {
		config.NSQ.NSQDAddress = value
	}
	if value := os.Getenv("TRIBEWAY_NSQLOOKUPD_ADDRESS"); value != "" {
		config.NSQ.NSQLookupDAddress = value
	}
	if value := os.Getenv("TRIBEWAY_ETCD_ENDPOINTS"); value != "" {
		config.ETCD.Endpoints = splitCSV(value)
	}
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			values = append(values, part)
		}
	}
	return values
}
