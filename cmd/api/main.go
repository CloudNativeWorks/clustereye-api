package main

import (
	"context"
	"database/sql"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/CloudNativeWorks/clustereye-api/internal/api"
	"github.com/CloudNativeWorks/clustereye-api/internal/config"
	"github.com/CloudNativeWorks/clustereye-api/internal/database"
	"github.com/CloudNativeWorks/clustereye-api/internal/logger"
	"github.com/CloudNativeWorks/clustereye-api/internal/metrics"
	"github.com/CloudNativeWorks/clustereye-api/internal/server"
	pb "github.com/CloudNativeWorks/clustereye-api/pkg/agent"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
)

// AgentConnection, bağlı bir agent'ı temsil eder
type AgentConnection struct {
	stream pb.AgentService_ConnectServer
	info   *pb.AgentInfo
}

// QueryResponse, sorgu sonuçlarını temsil eder
type QueryResponse struct {
	Result     string
	ResultChan chan *pb.QueryResult
}

type Server struct {
	pb.UnimplementedAgentServiceServer
	mu          sync.RWMutex
	agents      map[string]*AgentConnection
	queryMu     sync.RWMutex
	queryResult map[string]*QueryResponse
	db          *sql.DB
	companyRepo *database.CompanyRepository
}

func NewServer(db *sql.DB) *Server {
	return &Server{
		agents:      make(map[string]*AgentConnection),
		queryResult: make(map[string]*QueryResponse),
		db:          db,
		companyRepo: database.NewCompanyRepository(db),
	}
}

func main() {
	// Konfigürasyon yükleniyor
	cfg, err := config.LoadServerConfig()
	if err != nil {
		panic("Konfigürasyon yüklenemedi: " + err.Error())
	}

	// Logger'ı initialize et
	if err := logger.InitLogger(cfg.Log); err != nil {
		panic("Logger initialize edilemedi: " + err.Error())
	}

	logger.Info().Msg("ClusterEye API Server başlatılıyor")

	// Encryption'ı initialize et
	if err := api.InitEncryption(); err != nil {
		logger.Fatal().Err(err).Msg("Encryption initialize edilemedi")
	}

	// Veritabanı başlatma ve bağlantısı
	dbConfig := database.Config{
		Host:     cfg.Database.Host,
		Port:     cfg.Database.Port,
		User:     cfg.Database.User,
		Password: cfg.Database.Password,
		DBName:   cfg.Database.DBName,
		SSLMode:  cfg.Database.SSLMode,
	}

	db, err := database.InitDatabase(dbConfig)
	if err != nil {
		logger.Fatal().Err(err).Msg("Veritabanı başlatılamadı")
	}
	defer db.Close()

	// Tabloları oluştur
	if err := database.InitAllTables(db); err != nil {
		logger.Fatal().Err(err).Msg("Veritabanı tabloları oluşturulamadı")
	}

	logger.Info().Str("host", cfg.Database.Host).Int("port", cfg.Database.Port).Msg("Veritabanı bağlantısı kuruldu ve tablolar hazırlandı")

	// InfluxDB Writer'ı başlat
	influxWriter, err := metrics.NewInfluxDBWriter(cfg.InfluxDB)
	if err != nil {
		logger.Fatal().Err(err).Msg("InfluxDB bağlantısı kurulamadı")
	}
	defer influxWriter.Close()

	// gRPC Server başlat
	listener, err := net.Listen("tcp", cfg.GRPC.Address)
	if err != nil {
		logger.Fatal().Err(err).Str("address", cfg.GRPC.Address).Msg("gRPC listener başlatılamadı")
	}

	// gRPC sunucu seçeneklerini ayarla
	maxMsgSize := 128 * 1024 * 1024 // 128MB
	opts := []grpc.ServerOption{
		grpc.MaxRecvMsgSize(maxMsgSize),
		grpc.MaxSendMsgSize(maxMsgSize),

		// Keepalive enforcement policy - ENHANCE_YOUR_CALM hatasını önlemek için
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             60 * time.Second, // Minimum interval between pings
			PermitWithoutStream: false,            // Require active streams for pings
		}),

		// Server keepalive parameters
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle:     2 * time.Minute,  // Close idle connections after 2 minutes
			MaxConnectionAge:      30 * time.Minute, // Close connections after 30 minutes
			MaxConnectionAgeGrace: 5 * time.Minute,  // Grace period for closing connections
			Time:                  60 * time.Second, // Send pings every 60 seconds
			Timeout:               10 * time.Second, // Wait 10 seconds for ping response
		}),
	}

	grpcServer := grpc.NewServer(opts...)
	serverInstance := server.NewServer(db, influxWriter)
	pb.RegisterAgentServiceServer(grpcServer, serverInstance)

	go func() {
		logger.Info().Str("address", cfg.GRPC.Address).Msg("Cloud API gRPC server çalışıyor")
		if err := grpcServer.Serve(listener); err != nil {
			logger.Fatal().Err(err).Msg("gRPC server hatası")
		}
	}()

	// HTTP Gin API Server başlat
	router := gin.Default()

	// 🛡️ GÜVENLİK MİDDLEWARE'LERİ EKLE
	// Request logging middleware
	router.Use(api.RequestLoggingMiddleware())

	// Security headers middleware
	router.Use(api.SecurityHeadersMiddleware())

	// Rate limiting middleware - dakikada 120 istek
	router.Use(api.RateLimitMiddleware(120, time.Minute))

	// CORS middleware'ini yapılandır
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173", "http://localhost:3000", "http://localhost:8080", "https://demoui.clustereye.com", "https://demoapi.clustereye.com", "https://clabapi.clustereye.com", "https://commercelab.clustereye.com", "https://mlpapi.clustereye.com", "https://mlp.clustereye.com", "https://mytechnicapi.clustereye.com", "https://mytechnic.clustereye.com"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "Cookie"},
		ExposeHeaders:    []string{"Content-Length", "Authorization"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	// API handler'larını kaydet
	api.RegisterHandlers(router, serverInstance)

	// HTTP sunucusunu başlat
	logger.Info().Str("address", cfg.HTTP.Address).Msg("HTTP API server çalışıyor")
	if err := http.ListenAndServe(cfg.HTTP.Address, router); err != nil {
		logger.Fatal().Err(err).Msg("HTTP server hatası")
	}
}

// Connect, agent'ların bağlanması için stream açar
func (s *Server) Connect(stream pb.AgentService_ConnectServer) error {
	var currentAgentID string
	var companyID int

	for {
		in, err := stream.Recv()
		if err != nil {
			logger.Error().Str("agent_id", currentAgentID).Err(err).Msg("Agent bağlantısı kapandı")
			s.mu.Lock()
			delete(s.agents, currentAgentID)
			s.mu.Unlock()
			return err
		}

		switch payload := in.Payload.(type) {
		case *pb.AgentMessage_AgentInfo:
			agentInfo := payload.AgentInfo

			// Agent anahtarını doğrula
			company, err := s.companyRepo.ValidateAgentKey(context.Background(), agentInfo.Key)
			if err != nil {
				// Hata durumunda agent'a bildir
				errMsg := "Geçersiz agent anahtarı"
				if err == database.ErrKeyExpired {
					errMsg = "Agent anahtarı süresi dolmuş"
				}

				// Hata mesajını agent'a gönder
				stream.Send(&pb.ServerMessage{
					Payload: &pb.ServerMessage_Error{
						Error: &pb.Error{
							Code:    "AUTH_ERROR",
							Message: errMsg,
						},
					},
				})

				logger.Error().Str("agent_id", agentInfo.AgentId).Err(err).Msg("Agent kimlik doğrulama hatası")
				return err
			}

			// Agent ID'yi belirle
			currentAgentID = agentInfo.AgentId
			companyID = company.ID

			// Agent'ı kaydet
			err = s.companyRepo.RegisterAgent(
				context.Background(),
				companyID,
				currentAgentID,
				agentInfo.Hostname,
				agentInfo.Ip,
			)

			if err != nil {
				logger.Error().Str("agent_id", currentAgentID).Err(err).Msg("Agent kaydedilemedi")
				stream.Send(&pb.ServerMessage{
					Payload: &pb.ServerMessage_Error{
						Error: &pb.Error{
							Code:    "REGISTRATION_ERROR",
							Message: "Agent kaydedilemedi",
						},
					},
				})
				return err
			}

			// Agent'ı bağlantı listesine ekle
			s.mu.Lock()
			s.agents[currentAgentID] = &AgentConnection{
				stream: stream,
				info:   agentInfo,
			}
			s.mu.Unlock()

			// Başarılı kayıt mesajı gönder
			stream.Send(&pb.ServerMessage{
				Payload: &pb.ServerMessage_Registration{
					Registration: &pb.RegistrationResult{
						Status:  "success",
						Message: "Agent başarıyla kaydedildi",
					},
				},
			})

			logger.Info().
				Str("agent_id", currentAgentID).
				Str("hostname", agentInfo.Hostname).
				Str("ip", agentInfo.Ip).
				Str("company", company.CompanyName).
				Msg("Yeni Agent bağlandı")

		case *pb.AgentMessage_QueryResult:
			// Mevcut sorgu sonucu işleme kodu...
		}
	}
}
