package main

import (
	"context"
	"database/sql"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/mattn/go-sqlite3" // SQLite驱动
	"github.com/gin-gonic/gin"
	"github.com/q23818/ETHShanghai-2025/projects/w3hub/pkg/alert"
	"github.com/q23818/ETHShanghai-2025/projects/w3hub/pkg/asset"
	"github.com/q23818/ETHShanghai-2025/projects/w3hub/pkg/blockchain"
	"github.com/q23818/ETHShanghai-2025/projects/w3hub/pkg/blockchain/ethereum"
)

// blockchainWrapper 包装以太坊客户端以完整实现Client接口
type blockchainWrapper struct {
	client *ethereum.EthereumClient
}

func (w *blockchainWrapper) GetBalance(ctx context.Context, address string) (float64, error) {
	return w.client.GetBalance(ctx, address)
}

func (w *blockchainWrapper) GetAssets(ctx context.Context, address string) ([]blockchain.Asset, error) {
	return w.client.GetAssets(ctx, address)
}

func (w *blockchainWrapper) GetTransactions(ctx context.Context, address string, from, to time.Time) ([]blockchain.Transaction, error) {
	return w.client.GetTransactions(ctx, address, from, to)
}

func (w *blockchainWrapper) WatchAddress(ctx context.Context, address string) (<-chan blockchain.Transaction, error) {
	return w.client.WatchAddress(ctx, address)
}

func (w *blockchainWrapper) GetClient(chainType string) (blockchain.Client, bool) {
	if chainType == "ethereum" {
		return w, true // 返回wrapper本身而不是底层client
	}
	return nil, false
}

func main() {
	// 初始化配置
	config, err := loadConfig()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 初始化日志
	logger := log.New(os.Stdout, "W3HUB: ", log.LstdFlags|log.Lshortfile)

	// 初始化数据库
	db, err := sql.Open("sqlite3", config.DatabasePath)
	if err != nil {
		logger.Fatalf("数据库初始化失败: %v", err)
	}
	defer db.Close()

	// 初始化表结构
	if err := initDatabase(db); err != nil {
		logger.Fatalf("数据库初始化失败: %v", err)
	}

	// 初始化区块链客户端
	ethClient, err := ethereum.NewEthereumClient(config.EthRPCURL)
	if err != nil {
		logger.Fatalf("以太坊客户端初始化失败: %v", err)
	}

	// 初始化多链客户端
	chainClient := blockchain.NewMultiChainClient()
	
	// 包装以太坊客户端以符合Client接口
	wrappedEthClient := &blockchainWrapper{
		client: ethClient,
	}
	chainClient.RegisterClient("ethereum", wrappedEthClient)

	// 初始化服务
	assetManager := asset.NewManager(db, wrappedEthClient)
	// 通知服务初始化
	notifier := alert.NewMultiNotifier(
		&alert.EmailNotifier{
			SMTPHost:     os.Getenv("SMTP_HOST"),
			SMTPPort:     587,
			FromAddress:  os.Getenv("EMAIL_FROM"),
			FromPassword: os.Getenv("EMAIL_PASSWORD"),
		},
		&alert.TelegramNotifier{
			BotToken: os.Getenv("TELEGRAM_TOKEN"),
			ChatID:   os.Getenv("TELEGRAM_CHAT_ID"),
		},
	)
	
	// 将notifier传递给需要它的服务
	_ = notifier // 暂时保留但标记为使用

	// 初始化API服务
	api := NewAPI(assetManager)
	
	// 初始化Gin路由
	router := gin.Default()
	router.GET("/api/v1/assets/:chain/:address", api.GetAssets)
	router.GET("/api/v1/assets/history/:id", api.GetAssetHistory)

	go func() {
		if err := router.Run(":8080"); err != nil {
			logger.Fatalf("API服务启动失败: %v", err)
		}
	}()

	// 监控配置的地址
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	addresses := []string{"0xYourAddress1", "0xYourAddress2"}
	if err := assetManager.TrackAssets(ctx, "ethereum", addresses); err != nil {
		log.Fatal(err)
	}

	<-ctx.Done()
	log.Println("Shutting down...")
}