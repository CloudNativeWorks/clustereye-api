package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CloudNativeWorks/clustereye-api/internal/database"
	"github.com/CloudNativeWorks/clustereye-api/internal/logger"
	"github.com/CloudNativeWorks/clustereye-api/internal/metrics"
	pb "github.com/CloudNativeWorks/clustereye-api/pkg/agent"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api/write"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// PostgresInfo, PostgreSQL test sonucunu ve bilgilerini temsil eder
type PostgresInfo struct {
	Status   string `json:"status"`
	User     string `json:"user"`
	Password string `json:"password"`
	Cluster  string `json:"cluster"`
}

// AgentConnection, bir agent ile olan bağlantıyı temsil eder
type AgentConnection struct {
	Stream pb.AgentService_ConnectServer
	Info   *pb.AgentInfo
}

// QueryResponse, bir sorgu sonucunu ve sonuç kanalını içerir
type QueryResponse struct {
	Result     string
	ResultChan chan *pb.QueryResult
}

// Server, ClusterEye sunucusunu temsil eder
type Server struct {
	pb.UnimplementedAgentServiceServer
	mu          sync.RWMutex
	agents      map[string]*AgentConnection
	queryMu     sync.RWMutex
	queryResult map[string]*QueryResponse // query_id -> QueryResponse
	db          *sql.DB                   // PostgreSQL veritabanı bağlantısı
	companyRepo *database.CompanyRepository
	// Son ping zamanlarını tutmak için map
	lastPingMu   sync.RWMutex
	lastPingTime map[string]time.Time
	// Job yönetimi için yeni alanlar
	jobMu sync.RWMutex
	jobs  map[string]*pb.Job
	// InfluxDB writer
	influxWriter *metrics.InfluxDBWriter
	// Coordination duplicate prevention
	coordinationMu         sync.RWMutex
	processedCoordinations map[string]time.Time // key: process_id + old_master + new_master, value: timestamp
}

// NewServer, yeni bir sunucu nesnesi oluşturur
func NewServer(db *sql.DB, influxWriter *metrics.InfluxDBWriter) *Server {
	return &Server{
		agents:                 make(map[string]*AgentConnection),
		queryResult:            make(map[string]*QueryResponse),
		db:                     db,
		companyRepo:            database.NewCompanyRepository(db),
		lastPingTime:           make(map[string]time.Time),
		jobs:                   make(map[string]*pb.Job),
		influxWriter:           influxWriter,
		processedCoordinations: make(map[string]time.Time), // Coordination tracking
	}
}

// Connect, agent'ların bağlanması için stream açar
func (s *Server) Connect(stream pb.AgentService_ConnectServer) error {
	var currentAgentID string
	var companyID int

	logger.Info().Msg("Yeni agent bağlantı isteği alındı")

	for {
		in, err := stream.Recv()
		if err != nil {
			logger.Error().Err(err).Str("agent_id", currentAgentID).Msg("Agent bağlantısı kapandı")

			// Agent'ı silmek yerine, sadece Stream'i nil yap ve bağlantı durumunu güncelle
			if currentAgentID != "" {
				s.mu.Lock()
				if conn, exists := s.agents[currentAgentID]; exists && conn != nil {
					// Stream'i nil yap ama agent bilgilerini sakla
					conn.Stream = nil
				}
				s.mu.Unlock()

				// Son ping zamanını sıfırla ki "disconnected" olarak görünsün
				s.lastPingMu.Lock()
				delete(s.lastPingTime, currentAgentID)
				s.lastPingMu.Unlock()

				// Agent'ın veritabanındaki durumunu güncelle
				s.UpdateAgentDisconnectedStatus(currentAgentID)

				logger.Info().Str("agent_id", currentAgentID).Msg("Agent bağlantısı kapatıldı, disconnected olarak işaretlendi")
			}

			return err
		}

		switch payload := in.Payload.(type) {
		case *pb.AgentMessage_AgentInfo:
			agentInfo := payload.AgentInfo
			logger.Info().
				Str("agent_id", agentInfo.AgentId).
				Str("hostname", agentInfo.Hostname).
				Msg("Agent bilgileri alındı")

			// Agent anahtarını doğrula
			company, err := s.companyRepo.ValidateAgentKey(context.Background(), agentInfo.Key)
			if err != nil {
				logger.Error().Err(err).Str("agent_id", agentInfo.AgentId).Msg("Agent kimlik doğrulama hatası")
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
				logger.Error().Err(err).Str("agent_id", currentAgentID).Msg("Agent kaydedilemedi")
				return err
			}

			// Agent'ı bağlantı listesine ekle
			s.mu.Lock()
			s.agents[currentAgentID] = &AgentConnection{
				Stream: stream,
				Info:   agentInfo,
			}
			s.mu.Unlock()

			// İlk bağlantıda ping zamanını başlat
			s.lastPingMu.Lock()
			s.lastPingTime[currentAgentID] = time.Now()
			s.lastPingMu.Unlock()

			logger.Info().
				Str("agent_id", currentAgentID).
				Int("total_connections", len(s.agents)).
				Msg("Agent bağlantı listesine eklendi")

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
				Str("company", company.CompanyName).
				Msg("Agent başarıyla kaydedildi ve bağlandı")

		case *pb.AgentMessage_QueryResult:
			queryResult := payload.QueryResult
			logger.Debug().Str("agent_id", currentAgentID).Msg("Agent sorguya cevap verdi")

			// Ping yanıtlarını kontrol et
			if strings.HasPrefix(queryResult.QueryId, "ping_") {
				// Ping yanıtı alındı, son ping zamanını güncelle
				s.lastPingMu.Lock()
				s.lastPingTime[currentAgentID] = time.Now()
				s.lastPingMu.Unlock()

				// Veritabanındaki agent durumunu ve last_seen'i güncelle
				s.UpdateAgentConnectedStatus(currentAgentID)

				logger.Debug().Str("agent_id", currentAgentID).Msg("Ping yanıtı alındı, bağlantı durumu güncellendi")
				continue
			}

			// Sorgu sonucunu ilgili kanal üzerinden ilet
			s.queryMu.RLock()
			queryResp, ok := s.queryResult[queryResult.QueryId]
			s.queryMu.RUnlock()

			if ok && queryResp.ResultChan != nil {
				queryResp.ResultChan <- queryResult
			}
		}
	}
}

// Register, agent'ın kaydı için kullanılan gRPC metodudur
func (s *Server) Register(ctx context.Context, req *pb.RegisterRequest) (*pb.RegisterResponse, error) {
	logger.Info().Msg("Register metodu çağrıldı")
	agentInfo := req.AgentInfo

	// Agent anahtarını doğrula
	company, err := s.companyRepo.ValidateAgentKey(ctx, agentInfo.Key)
	if err != nil {
		logger.Error().Err(err).Str("agent_id", agentInfo.AgentId).Msg("Agent kimlik doğrulama hatası")
		return &pb.RegisterResponse{
			Registration: &pb.RegistrationResult{
				Status:  "error",
				Message: "Geçersiz agent anahtarı",
			},
		}, nil
	}

	// Agent'ı kaydet
	err = s.companyRepo.RegisterAgent(
		ctx,
		company.ID,
		agentInfo.AgentId,
		agentInfo.Hostname,
		agentInfo.Ip,
	)

	if err != nil {
		logger.Error().Err(err).Str("agent_id", agentInfo.AgentId).Msg("Agent kaydedilemedi")
		return &pb.RegisterResponse{
			Registration: &pb.RegistrationResult{
				Status:  "error",
				Message: "Agent kaydedilemedi",
			},
		}, nil
	}

	// PostgreSQL bağlantı bilgilerini kaydet
	logger.Info().
		Str("hostname", agentInfo.Hostname).
		Str("cluster", agentInfo.Platform).
		Str("postgres_user", agentInfo.PostgresUser).
		Msg("PostgreSQL bilgileri kaydediliyor")

	// Veritabanı bağlantısını kontrol et
	if err := s.checkDatabaseConnection(); err != nil {
		logger.Error().Err(err).Msg("Veritabanı bağlantı hatası")
	}

	// PostgreSQL bilgilerini kaydet
	err = s.companyRepo.SavePostgresConnInfo(
		ctx,
		agentInfo.Hostname,
		agentInfo.Platform,     // Platform alanını cluster adı olarak kullanıyoruz
		agentInfo.PostgresUser, // Agent'dan gelen kullanıcı adı
		agentInfo.PostgresPass, // Agent'dan gelen şifre
	)

	if err != nil {
		logger.Error().Err(err).Str("hostname", agentInfo.Hostname).Msg("PostgreSQL bağlantı bilgileri kaydedilemedi")
	} else {
		logger.Info().Str("hostname", agentInfo.Hostname).Msg("PostgreSQL bağlantı bilgileri kaydedildi")
	}

	logger.Info().
		Str("agent_id", agentInfo.AgentId).
		Str("hostname", agentInfo.Hostname).
		Str("company", company.CompanyName).
		Msg("Yeni Agent bağlandı ve kaydedildi")

	return &pb.RegisterResponse{
		Registration: &pb.RegistrationResult{
			Status:  "success",
			Message: "Agent başarıyla kaydedildi",
		},
	}, nil
}

func (s *Server) ExecuteQuery(ctx context.Context, req *pb.QueryRequest) (*pb.QueryResponse, error) {
	// Sorgu işleme mantığı
	// ...
	return &pb.QueryResponse{
		Result: &pb.QueryResult{
			QueryId: req.Query.QueryId,
			// Sonuç verilerini doldurun
		},
	}, nil
}

func (s *Server) SendPostgresInfo(ctx context.Context, req *pb.PostgresInfoRequest) (*pb.PostgresInfoResponse, error) {
	logger.Info().Msg("SendPostgresInfo metodu çağrıldı")

	// Gelen PostgreSQL bilgilerini logla
	pgInfo := req.PostgresInfo
	
	// Agent ID'sini metrik verilerinden çıkar ve agent durumunu active yap
	agentID := fmt.Sprintf("agent_%s", pgInfo.Hostname)
	s.UpdateAgentConnectedStatus(agentID)
	logger.Debug().
		Str("cluster", pgInfo.ClusterName).
		Str("hostname", pgInfo.Hostname).
		Str("ip", pgInfo.Ip).
		Msg("PostgreSQL bilgileri alındı")

	// Daha detaylı loglama
	logger.Debug().
		Str("cluster", pgInfo.ClusterName).
		Str("ip", pgInfo.Ip).
		Str("hostname", pgInfo.Hostname).
		Str("node_status", pgInfo.NodeStatus).
		Str("pg_version", pgInfo.PgVersion).
		Str("location", pgInfo.Location).
		Str("pgbouncer_status", pgInfo.PgBouncerStatus).
		Str("pg_service_status", pgInfo.PgServiceStatus).
		Int64("replication_lag_sec", pgInfo.ReplicationLagSec).
		Str("free_disk", pgInfo.FreeDisk).
		Int64("fd_percent", int64(pgInfo.FdPercent)).
		Str("config_path", pgInfo.ConfigPath).
		Str("data_path", pgInfo.DataPath).
		Msg("PostgreSQL detay bilgileri")

	// Veritabanına kaydetme işlemi
	// Bu kısmı ihtiyacınıza göre geliştirebilirsiniz
	err := s.savePostgresInfoToDatabase(ctx, pgInfo)
	if err != nil {
		logger.Error().Err(err).Str("cluster", pgInfo.ClusterName).Msg("PostgreSQL bilgileri veritabanına kaydedilemedi")
		return &pb.PostgresInfoResponse{
			Status: "error",
		}, nil
	}

	logger.Info().Str("cluster", pgInfo.ClusterName).Msg("PostgreSQL bilgileri başarıyla işlendi ve kaydedildi")

	return &pb.PostgresInfoResponse{
		Status: "success",
	}, nil
}

// PostgreSQL bilgilerini veritabanına kaydetmek için yardımcı fonksiyon
func (s *Server) savePostgresInfoToDatabase(ctx context.Context, pgInfo *pb.PostgresInfo) error {
	// Önce mevcut kaydı kontrol et
	var existingData []byte
	var id int

	checkQuery := `
		SELECT id, jsondata FROM public.postgres_data 
		WHERE clustername = $1 
		ORDER BY id DESC LIMIT 1
	`

	err := s.db.QueryRowContext(ctx, checkQuery, pgInfo.ClusterName).Scan(&id, &existingData)

	// Yeni node verisi
	pgData := map[string]interface{}{
		"ClusterName":       pgInfo.ClusterName,
		"Location":          pgInfo.Location,
		"FDPercent":         pgInfo.FdPercent,
		"FreeDisk":          pgInfo.FreeDisk,
		"Hostname":          pgInfo.Hostname,
		"IP":                pgInfo.Ip,
		"NodeStatus":        pgInfo.NodeStatus,
		"PGBouncerStatus":   pgInfo.PgBouncerStatus,
		"PGServiceStatus":   pgInfo.PgServiceStatus,
		"PGVersion":         pgInfo.PgVersion,
		"ReplicationLagSec": pgInfo.ReplicationLagSec,
		"TotalVCPU":         pgInfo.TotalVcpu,
		"TotalMemory":       pgInfo.TotalMemory,
		"ConfigPath":        pgInfo.ConfigPath,
		"DataPath":          pgInfo.DataPath,
		// Patroni fields
		"PatroniEnabled":   pgInfo.PatroniEnabled,
		"PatroniCluster":   pgInfo.PatroniCluster,
		"PatroniRole":      pgInfo.PatroniRole,
		"PatroniState":     pgInfo.PatroniState,
		"PatroniRestAPI":   pgInfo.PatroniRestApi,
		"PatroniDetection": pgInfo.PatroniDetection,
	}

	var jsonData []byte

	// Hata kontrolünü düzgün yap
	if err == nil {
		// Mevcut kayıt var, güncelle
		logger.Debug().
			Str("cluster", pgInfo.ClusterName).
			Int("id", id).
			Msg("PostgreSQL cluster için mevcut kayıt bulundu, güncelleniyor")

		var existingJSON map[string][]interface{}
		if err := json.Unmarshal(existingData, &existingJSON); err != nil {
			logger.Error().Err(err).Str("cluster", pgInfo.ClusterName).Msg("Mevcut JSON ayrıştırma hatası")
			return err
		}

		// Cluster array'ini al
		clusterData, ok := existingJSON[pgInfo.ClusterName]
		if !ok {
			// Eğer cluster verisi yoksa yeni oluştur
			clusterData = []interface{}{}
		}

		// Node'u bul ve güncelle
		nodeFound := false
		nodeChanged := false
		for i, node := range clusterData {
			nodeMap, ok := node.(map[string]interface{})
			if !ok {
				continue
			}

			// Hostname ve IP ile node eşleşmesi kontrol et
			if nodeMap["Hostname"] == pgInfo.Hostname && nodeMap["IP"] == pgInfo.Ip {
				// Sadece değişen alanları güncelle
				nodeFound = true

				// Değişiklikleri takip et
				for key, newValue := range pgData {
					currentValue, exists := nodeMap[key]
					var hasChanged bool

					if !exists {
						// Değer mevcut değil, yeni alan ekleniyor
						hasChanged = true
						logger.Debug().
							Str("hostname", pgInfo.Hostname).
							Str("field", key).
							Msg("PostgreSQL node'da yeni alan eklendi")
					} else {
						// Mevcut değer ile yeni değeri karşılaştır
						// Numeric değerler için özel karşılaştırma yap
						hasChanged = !compareValues(currentValue, newValue)
						if hasChanged {
							logger.Debug().
								Str("hostname", pgInfo.Hostname).
								Str("field", key).
								Interface("old_value", currentValue).
								Interface("new_value", newValue).
								Msg("PostgreSQL node'da değişiklik tespit edildi")
						}
					}

					if hasChanged {
						nodeMap[key] = newValue
						// NodeStatus, PGServiceStatus, FreeDisk gibi önemli alanlar değiştiyse işaretle
						if key == "NodeStatus" || key == "PGServiceStatus" || key == "FreeDisk" || key == "ReplicationLagSec" || key == "PGBouncerStatus" {
							nodeChanged = true
						}
					}
				}

				clusterData[i] = nodeMap
				break
			}
		}

		// Eğer node bulunamadıysa yeni ekle
		if !nodeFound {
			clusterData = append(clusterData, pgData)
			nodeChanged = true
			logger.Info().Str("hostname", pgInfo.Hostname).Msg("Yeni PostgreSQL node eklendi")
		}

		// Eğer önemli bir değişiklik yoksa veritabanını güncelleme
		if !nodeChanged {
			logger.Debug().Str("hostname", pgInfo.Hostname).Msg("PostgreSQL node'da önemli bir değişiklik yok, güncelleme yapılmadı")
			return nil
		}

		existingJSON[pgInfo.ClusterName] = clusterData

		// JSON'ı güncelle
		jsonData, err = json.Marshal(existingJSON)
		if err != nil {
			logger.Error().Err(err).Str("cluster", pgInfo.ClusterName).Msg("JSON dönüştürme hatası")
			return err
		}

		// Veritabanını güncelle
		updateQuery := `
			UPDATE public.postgres_data 
			SET jsondata = $1, updated_at = CURRENT_TIMESTAMP
			WHERE id = $2
		`

		_, err = s.db.ExecContext(ctx, updateQuery, jsonData, id)
		if err != nil {
			logger.Error().Err(err).Str("cluster", pgInfo.ClusterName).Msg("Veritabanı güncelleme hatası")
			return err
		}

		logger.Info().Str("cluster", pgInfo.ClusterName).Msg("PostgreSQL node bilgileri başarıyla güncellendi (önemli değişiklik nedeniyle)")
	} else if err == sql.ErrNoRows {
		// Kayıt bulunamadı, yeni kayıt oluştur
		logger.Info().Str("cluster", pgInfo.ClusterName).Msg("PostgreSQL cluster için kayıt bulunamadı, yeni kayıt oluşturuluyor")

		outerJSON := map[string][]interface{}{
			pgInfo.ClusterName: {pgData},
		}

		jsonData, err = json.Marshal(outerJSON)
		if err != nil {
			logger.Error().Err(err).Str("cluster", pgInfo.ClusterName).Msg("JSON dönüştürme hatası")
			return err
		}

		insertQuery := `
			INSERT INTO public.postgres_data (
				jsondata, clustername, created_at, updated_at
			) VALUES ($1, $2, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
			ON CONFLICT (clustername) DO UPDATE SET
				jsondata = EXCLUDED.jsondata,
				updated_at = CURRENT_TIMESTAMP
		`

		_, err = s.db.ExecContext(ctx, insertQuery, jsonData, pgInfo.ClusterName)
		if err != nil {
			logger.Error().Err(err).Str("cluster", pgInfo.ClusterName).Msg("Veritabanı ekleme hatası")
			return err
		}

		logger.Info().Str("cluster", pgInfo.ClusterName).Msg("PostgreSQL node bilgileri başarıyla veritabanına kaydedildi (yeni kayıt)")
	} else {
		// Başka bir veritabanı hatası oluştu
		logger.Error().Err(err).Str("cluster", pgInfo.ClusterName).Msg("PostgreSQL cluster kayıt kontrolü sırasında hata")
		return fmt.Errorf("veritabanı kontrol hatası: %v", err)
	}

	return nil
}

func (s *Server) StreamQueries(stream pb.AgentService_StreamQueriesServer) error {
	// Sürekli sorgu akışı mantığı
	// ...
	return nil
}

func (s *Server) StreamPostgresInfo(stream pb.AgentService_StreamPostgresInfoServer) error {
	// Sürekli PostgreSQL bilgi akışı mantığı
	// ...
	return nil
}

// GetStatusPostgres, PostgreSQL veritabanından durum bilgilerini çeker
func (s *Server) GetStatusPostgres(ctx context.Context, _ *structpb.Struct) (*structpb.Value, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT json_agg(sub.jsondata) FROM (SELECT jsondata FROM postgres_data ORDER BY id) AS sub")
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Veritabanı sorgusu başarısız: %v", err)
	}
	defer rows.Close()

	var jsonData []byte
	if rows.Next() {
		err := rows.Scan(&jsonData)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Veri okuma hatası: %v", err)
		}
	}

	// JSON verisini structpb.Value'ya dönüştür
	var jsonValue interface{}
	if err := json.Unmarshal(jsonData, &jsonValue); err != nil {
		return nil, status.Errorf(codes.Internal, "JSON ayrıştırma hatası: %v", err)
	}

	value, err := structpb.NewValue(jsonValue)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Veri dönüştürme hatası: %v", err)
	}

	return value, nil
}

// GetConnectedAgents, aktif gRPC bağlantılarındaki agent'ları döndürür
func (s *Server) GetConnectedAgents() []map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	logger.Debug().Int("active_connections", len(s.agents)).Msg("Aktif gRPC bağlantıları")

	// Istanbul zaman dilimini al
	loc, err := time.LoadLocation("Europe/Istanbul")
	if err != nil {
		logger.Warn().Err(err).Msg("Zaman dilimi yüklenemedi")
		loc = time.UTC
	}

	agents := make([]map[string]interface{}, 0)
	for id, conn := range s.agents {
		if conn == nil || conn.Info == nil {
			logger.Warn().Str("agent_id", id).Msg("Geçersiz agent bağlantısı")
			continue
		}

		// gRPC stream'in durumunu kontrol et
		status := "disconnected"
		// Son ping zamanını al
		s.lastPingMu.RLock()
		lastPing, exists := s.lastPingTime[id]
		s.lastPingMu.RUnlock()

		// Varsayılan olarak son görülme zamanını son ping zamanı olarak ayarla
		lastSeenTime := lastPing

		if conn.Stream != nil {
			shouldPing := false

			// Ping frekansını azalt - ENHANCE_YOUR_CALM hatasını önlemek için
			if !exists || time.Since(lastPing) > 60*time.Second {
				shouldPing = true
			} else {
				// Son 60 saniye içinde başarılı ping varsa, bağlı kabul et
				status = "connected"
			}

			if shouldPing {
				// Ping mesajı gönder
				pingMsg := &pb.ServerMessage{
					Payload: &pb.ServerMessage_Query{
						Query: &pb.Query{
							QueryId: fmt.Sprintf("ping_%d", time.Now().UnixNano()),
							Command: "ping",
						},
					},
				}

				// Ping'i sadece gRPC stream üzerinden gönder
				err := conn.Stream.Send(pingMsg)
				if err == nil {
					// Ping gönderimi başarılı olduğunda da bağlantıyı "connected" olarak işaretle
					// Agent yanıt verdiğinde Connect metodu lastPingTime'ı güncelleyecek
					status = "connected"
					// Başarılı ping zamanını kaydet
					s.lastPingMu.Lock()
					s.lastPingTime[id] = time.Now()
					lastSeenTime = s.lastPingTime[id] // Son görülme zamanını güncelle
					s.lastPingMu.Unlock()

					// Veritabanındaki agent durumunu ve last_seen'i güncelle
					s.UpdateAgentConnectedStatus(id)

					logger.Debug().Str("agent_id", id).Msg("Ping gönderimi başarılı, bağlantı durumu güncellendi")
				} else {
					logger.Warn().Err(err).Str("agent_id", id).Msg("Agent ping hatası")
					// Stream'i nil yap ama agent'ı silme
					conn.Stream = nil
					// Son ping zamanını da sil
					s.lastPingMu.Lock()
					delete(s.lastPingTime, id)
					s.lastPingMu.Unlock()
					// Status zaten "disconnected" olarak ayarlandı
				}
			}
		}

		// last_seen için doğru zaman bilgisini kullan
		var lastSeenStr string
		if exists {
			// Eğer son ping zamanı varsa, onu kullan
			lastSeenStr = lastSeenTime.In(loc).Format("2006-01-02T15:04:05-07:00")
		} else {
			// Eğer hiç ping alınmadıysa, şu anki zamanı kullan
			// Bu sadece yeni eklenen ve henüz ping almamış agentlar için geçerli olmalı
			lastSeenStr = time.Now().In(loc).Format("2006-01-02T15:04:05-07:00")
		}

		// Agent'ın veritabanındaki durumunu kontrol et
		var dbStatus string
		err := s.db.QueryRow("SELECT status FROM agents WHERE agent_id = $1", id).Scan(&dbStatus)
		if err == nil && dbStatus == "disconnected" {
			// Eğer veritabanında disconnected olarak işaretlenmişse, status'u disconnected olarak ayarla
			status = "disconnected"
		}

		agent := map[string]interface{}{
			"id":         id,
			"hostname":   conn.Info.Hostname,
			"ip":         conn.Info.Ip,
			"status":     status,
			"last_seen":  lastSeenStr,
			"connection": "grpc",
		}
		agents = append(agents, agent)
	}
	return agents
}

// GetAgentStatusFromDB, veritabanından agent durumlarını alır
func (s *Server) GetAgentStatusFromDB(ctx context.Context) ([]map[string]interface{}, error) {
	query := `
		SELECT hostname, last_seen 
		FROM agents 
		WHERE last_seen > NOW() - INTERVAL '1 minute'
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("veritabanı sorgusu başarısız: %v", err)
	}
	defer rows.Close()

	agents := make([]map[string]interface{}, 0)
	for rows.Next() {
		var hostname string
		var lastSeen time.Time
		if err := rows.Scan(&hostname, &lastSeen); err != nil {
			return nil, fmt.Errorf("satır okuma hatası: %v", err)
		}

		agent := map[string]interface{}{
			"hostname":   hostname,
			"last_seen":  lastSeen.Format(time.RFC3339),
			"status":     "active",
			"connection": "db",
		}
		agents = append(agents, agent)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("satır okuma hatası: %v", err)
	}

	return agents, nil
}

// Veritabanı bağlantısını kontrol et
func (s *Server) checkDatabaseConnection() error {
	err := s.db.Ping()
	if err != nil {
		logger.Error().Err(err).Msg("Veritabanı bağlantı hatası")
		return err
	}
	return nil
}

// UpdateAgentDisconnectedStatus, agent'ın veritabanındaki durumunu disconnected olarak günceller
func (s *Server) UpdateAgentDisconnectedStatus(agentID string) error {
	query := `
		UPDATE agents 
		SET status = 'disconnected'
		WHERE agent_id = $1
	`
	_, err := s.db.Exec(query, agentID)
	if err != nil {
		logger.Error().Err(err).Str("agent_id", agentID).Msg("Agent durumu veritabanında güncellenemedi")
	} else {
		logger.Info().Str("agent_id", agentID).Msg("Agent durumu veritabanında disconnected olarak güncellendi")
	}
	return err
}

// UpdateAgentConnectedStatus, agent'ın veritabanındaki durumunu active olarak günceller ve last_seen'i şimdiki zaman yapar
func (s *Server) UpdateAgentConnectedStatus(agentID string) error {
	query := `
		UPDATE agents 
		SET status = 'active', last_seen = CURRENT_TIMESTAMP
		WHERE agent_id = $1
	`
	_, err := s.db.Exec(query, agentID)
	if err != nil {
		logger.Error().Err(err).Str("agent_id", agentID).Msg("Agent durumu veritabanında güncellenemedi")
	} else {
		logger.Debug().Str("agent_id", agentID).Msg("Agent durumu veritabanında active olarak güncellendi ve last_seen güncellendi")
	}
	return err
}

// SendQuery, belirli bir agent'a sorgu gönderir ve cevabı bekler

// SendQuery, belirli bir agent'a sorgu gönderir ve cevabı bekler

// SendQuery, belirli bir agent'a sorgu gönderir ve cevabı bekler
func (s *Server) SendQuery(ctx context.Context, agentID, queryID, command, database string) (*pb.QueryResult, error) {

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Agent ID'sini kontrol et ve gerekirse düzelt
	if !strings.HasPrefix(agentID, "agent_") {
		agentID = "agent_" + agentID
	}

	agentConn, ok := s.agents[agentID]
	if !ok {
		return nil, fmt.Errorf("agent bulunamadı: %s", agentID)
	}

	// Sorgu cevabı için bir kanal oluştur
	resultChan := make(chan *pb.QueryResult, 1)

	// Haritaya ekle
	s.queryMu.Lock()
	s.queryResult[queryID] = &QueryResponse{
		ResultChan: resultChan,
	}
	s.queryMu.Unlock()

	// Temizlik işlemi için defer
	defer func() {
		s.queryMu.Lock()
		delete(s.queryResult, queryID)
		s.queryMu.Unlock()
		close(resultChan)
	}()

	// Sorguyu agent'a gönder
	err := agentConn.Stream.Send(&pb.ServerMessage{
		Payload: &pb.ServerMessage_Query{
			Query: &pb.Query{
				QueryId:  queryID,
				Command:  command,
				Database: database,
			},
		},
	})

	if err != nil {
		logger.Error().Err(err).Str("query_id", queryID).Msg("Sorgu gönderimi başarısız")
		return nil, err
	}

	logger.Debug().Str("query_id", queryID).Msg("Sorgu gönderildi, yanıt bekleniyor")

	// Cevabı bekle (timeout ile)
	select {
	case result := <-resultChan:
		// Protobuf sonucunu JSON formatına dönüştür
		if result.Result != nil {
			// Protobuf struct'ı parse et
			var structValue structpb.Struct
			if err := result.Result.UnmarshalTo(&structValue); err != nil {
				logger.Error().Err(err).Str("query_id", queryID).Msg("Error unmarshaling to struct")
				return result, nil
			}

			// Struct'ı map'e dönüştür
			resultMap := structValue.AsMap()

			// Map'i JSON'a dönüştür
			jsonBytes, err := json.Marshal(resultMap)
			if err != nil {
				logger.Error().Err(err).Str("query_id", queryID).Msg("Error marshaling map to JSON")
				return result, nil
			}

			// Sonucu güncelle
			result.Result = &anypb.Any{
				TypeUrl: "type.googleapis.com/google.protobuf.Value",
				Value:   jsonBytes,
			}
		}
		logger.Debug().Str("query_id", queryID).Msg("Sorgu yanıtı alındı")
		return result, nil
	case <-ctx.Done():
		logger.Error().Err(ctx.Err()).Str("query_id", queryID).Msg("Context iptal edildi")
		return nil, ctx.Err()
	case <-time.After(60 * time.Second): // 60 saniye timeout - uzun süren sorgular için
		logger.Error().Str("query_id", queryID).Msg("Sorgu zaman aşımına uğradı")
		return nil, fmt.Errorf("sorgu zaman aşımına uğradı")
	}
}

// SendSystemMetrics, agent'dan sistem metriklerini alır
func (s *Server) SendSystemMetrics(ctx context.Context, req *pb.SystemMetricsRequest) (*pb.SystemMetricsResponse, error) {
	logger.Info().Str("agent_id", req.AgentId).Msg("SendSystemMetrics başladı - basitleştirilmiş yaklaşım")

	// Agent ID'yi standart formata getir
	agentID := req.AgentId
	if !strings.HasPrefix(agentID, "agent_") {
		agentID = "agent_" + agentID
		logger.Debug().Str("agent_id", agentID).Msg("Agent ID düzeltildi")
	}

	// Agent bağlantısını bul
	s.mu.RLock()
	agentConn, ok := s.agents[agentID]
	s.mu.RUnlock()

	if !ok {
		logger.Error().Str("agent_id", agentID).Msg("Agent bulunamadı")
		return nil, fmt.Errorf("agent bulunamadı: %s", agentID)
	}

	if agentConn.Stream == nil {
		logger.Error().Str("agent_id", agentID).Msg("Agent stream bağlantısı yok")
		return nil, fmt.Errorf("agent stream bağlantısı yok: %s", agentID)
	}

	// Metrics için bir kanal oluştur
	metricsChan := make(chan *pb.SystemMetrics, 1)
	errorChan := make(chan error, 1)

	// Unique bir query ID oluştur
	queryID := fmt.Sprintf("metrics_%d", time.Now().UnixNano())

	// Query sonucu için bir kanal oluştur
	resultChan := make(chan *pb.QueryResult, 1)
	s.queryMu.Lock()
	s.queryResult[queryID] = &QueryResponse{
		ResultChan: resultChan,
	}
	s.queryMu.Unlock()

	// İşlem bitince cleanup yap
	defer func() {
		s.queryMu.Lock()
		delete(s.queryResult, queryID)
		s.queryMu.Unlock()
		close(resultChan)
		close(metricsChan)
		close(errorChan)
	}()

	logger.Info().Str("query_id", queryID).Msg("Metrik almak için özel sorgu gönderiliyor")

	// Metrik almak için "get_system_metrics" adında özel bir sorgu gönder
	err := agentConn.Stream.Send(&pb.ServerMessage{
		Payload: &pb.ServerMessage_Query{
			Query: &pb.Query{
				QueryId:  queryID,
				Command:  "get_system_metrics",
				Database: "",
			},
		},
	})

	if err != nil {
		logger.Error().Err(err).Str("query_id", queryID).Msg("Metrik sorgusu gönderilemedi")
		return nil, fmt.Errorf("metrik sorgusu gönderilemedi: %v", err)
	}

	logger.Info().Str("query_id", queryID).Msg("Metrik sorgusu gönderildi, yanıt bekleniyor")

	// 3 saniyelik timeout ayarla
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// Yanıtı bekle
	select {
	case result := <-resultChan:
		if result == nil {
			logger.Error().Str("query_id", queryID).Msg("Boş metrik yanıtı alındı")
			return nil, fmt.Errorf("boş metrik yanıtı alındı")
		}

		logger.Info().Str("query_id", queryID).Msg("Metrik yanıtı alındı")

		// Result içerisindeki Any tipini Struct'a dönüştür
		var metricsStruct structpb.Struct
		if err := result.Result.UnmarshalTo(&metricsStruct); err != nil {
			logger.Error().Err(err).Str("query_id", queryID).Msg("Metrik yapısı çözümlenemedi")
			return nil, fmt.Errorf("metrik yapısı çözümlenemedi: %v", err)
		}
		// Yanıtı oluştur ve döndür
		response := &pb.SystemMetricsResponse{
			Status: "success",
			Data:   &metricsStruct,
		}

		logger.Info().Str("query_id", queryID).Msg("Metrik yanıtı başarıyla döndürülüyor")
		return response, nil

	case <-ctx.Done():
		logger.Error().Err(ctx.Err()).Str("query_id", queryID).Msg("Metrik yanıtı beklerken timeout")
		return nil, ctx.Err()
	}
}

// GetDB, veritabanı bağlantısını döndürür
func (s *Server) GetDB() *sql.DB {
	return s.db
}

// GetInfluxWriter, InfluxDB writer'ını döndürür
func (s *Server) GetInfluxWriter() *metrics.InfluxDBWriter {
	return s.influxWriter
}

// SendMetrics, agent'dan gelen metric batch'lerini işler
func (s *Server) SendMetrics(ctx context.Context, req *pb.SendMetricsRequest) (*pb.SendMetricsResponse, error) {
	if req.Batch == nil {
		return &pb.SendMetricsResponse{
			Status:  "error",
			Message: "Batch is required",
		}, nil
	}

	batch := req.Batch

	var errors []string
	processedCount := int32(0)

	// InfluxDB'ye metrikler yazılacaksa
	if s.influxWriter != nil {
		// InfluxDB yazma işlemleri için ayrı context - uzun timeout
		influxCtx, influxCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer influxCancel()
		// PostgreSQL active queries metadata'sını kontrol et ve işle
		if activeQueriesJSON, exists := batch.Metadata["active_queries"]; exists && batch.MetricType == "postgresql_database" {
			if err := s.processActiveQueriesMetadata(influxCtx, batch.AgentId, activeQueriesJSON); err != nil {
				errorMsg := fmt.Sprintf("Active queries metadata işleme hatası: %v", err)
				errors = append(errors, errorMsg)
				logger.Error().
					Str("agent_id", batch.AgentId).
					Err(err).
					Msg("Active queries metadata işleme hatası")
			} else {
				logger.Debug().
					Str("agent_id", batch.AgentId).
					Msg("Active queries metadata başarıyla işlendi")
			}
		}

		// MongoDB active operations metadata'sını kontrol et ve işle
		if activeOperationsJSON, exists := batch.Metadata["active_operations"]; exists && batch.MetricType == "mongodb_database" {
			if err := s.processActiveOperationsMetadata(influxCtx, batch.AgentId, activeOperationsJSON); err != nil {
				errorMsg := fmt.Sprintf("Active operations metadata işleme hatası: %v", err)
				errors = append(errors, errorMsg)
				logger.Error().
					Str("agent_id", batch.AgentId).
					Err(err).
					Msg("Active operations metadata işleme hatası")
			} else {
				logger.Debug().
					Str("agent_id", batch.AgentId).
					Msg("Active operations metadata başarıyla işlendi")
			}
		}

		// Tüm metrikler için points oluştur ve batch olarak yaz
		if len(batch.Metrics) > 0 {
			if err := s.writeBatchMetricsToInfluxDB(influxCtx, batch); err != nil {
				// Batch yazma başarısız olursa, tekil yazma deneyeceğiz
				logger.Warn().
					Str("agent_id", batch.AgentId).
					Int("metric_count", len(batch.Metrics)).
					Err(err).
					Msg("Batch metric yazma başarısız, tekil yazma deneyecek")
				
				// Her metric'i tek tek yazmayı dene
				for _, metric := range batch.Metrics {
					if err := s.writeMetricToInfluxDB(influxCtx, batch, metric); err != nil {
						errorMsg := fmt.Sprintf("Metric yazma hatası (%s): %v", metric.Name, err)
						errors = append(errors, errorMsg)
						logger.Error().
							Str("metric_name", metric.Name).
							Str("agent_id", batch.AgentId).
							Err(err).
							Msg("Metric yazma hatası")
					} else {
						processedCount++
					}
				}
			} else {
				processedCount = int32(len(batch.Metrics))
				logger.Debug().
					Str("agent_id", batch.AgentId).
					Int("metric_count", len(batch.Metrics)).
					Msg("Batch metric yazma başarılı")
			}
		}
	} else {
		// InfluxDB yoksa sadece log'la
		for _, metric := range batch.Metrics {
			logger.Debug().
				Str("metric_name", metric.Name).
				Float64("value", s.getMetricValueAsFloat(metric.Value)).
				Str("agent_id", batch.AgentId).
				Msg("Metric alındı")
			processedCount++
		}
	}

	// Yanıt oluştur
	status := "success"
	message := fmt.Sprintf("Processed %d metrics", processedCount)

	if len(errors) > 0 {
		if processedCount == 0 {
			status = "error"
			message = "All metrics failed to process"
		} else {
			status = "partial_success"
			message = fmt.Sprintf("Processed %d metrics with %d errors", processedCount, len(errors))
		}
	}

	return &pb.SendMetricsResponse{
		Status:         status,
		Message:        message,
		ProcessedCount: processedCount,
		Errors:         errors,
	}, nil
}

// processActiveQueriesMetadata, metadata'daki active queries JSON'ını işler ve InfluxDB'ye yazar
func (s *Server) processActiveQueriesMetadata(ctx context.Context, agentID, activeQueriesJSON string) error {
	// JSON'ı parse et
	var queries []map[string]interface{}
	if err := json.Unmarshal([]byte(activeQueriesJSON), &queries); err != nil {
		return fmt.Errorf("active queries JSON parse hatası: %v", err)
	}

	logger.Debug().
		Str("agent_id", agentID).
		Int("query_count", len(queries)).
		Str("json_preview", activeQueriesJSON[:min(200, len(activeQueriesJSON))]).
		Msg("Active/completed queries metadata parse edildi")

		// Her query için InfluxDB point'i oluştur
	activeCount := 0
	completedCount := 0

	for i, queryData := range queries {
		// Agent'dan gelen timestamp'ı kullan, yoksa current time
		timestamp := time.Now()
		if timestampStr, ok := queryData["timestamp"].(string); ok && timestampStr != "" {
			if parsedTime, err := time.Parse(time.RFC3339Nano, timestampStr); err == nil {
				timestamp = parsedTime
			}
		}
		// Debug: field'ları ve tiplerini logla
		logger.Debug().
			Str("agent_id", agentID).
			Int("query_index", i).
			Interface("query_data", queryData).
			Msg("Query data debug")

		// PID tracking ile gelen yeni alanları işle
		pid := queryData["pid"]
		state, _ := queryData["state"].(string)

		// Query completion detection için state'i kontrol et
		if state == "completed" {
			completedCount++
			logger.Debug().
				Str("agent_id", agentID).
				Interface("pid", pid).
				Interface("duration", queryData["duration_seconds"]).
				Msg("Completed query detected")

			// Completed query'leri ayrı measurement'a yaz
			if err := s.writeCompletedQueryToInfluxDB(ctx, agentID, queryData, timestamp); err != nil {
				logger.Error().
					Str("agent_id", agentID).
					Err(err).
					Msg("Completed query InfluxDB yazma hatası")
				return err
			}
		} else {
			activeCount++
			// Active query'leri mevcut şekilde işle
			if err := s.writeActiveQueryToInfluxDB(ctx, agentID, queryData, timestamp); err != nil {
				logger.Error().
					Str("agent_id", agentID).
					Err(err).
					Msg("Active query InfluxDB yazma hatası")
				return err
			}
		}
	}

	logger.Debug().
		Str("agent_id", agentID).
		Int("active_queries", activeCount).
		Int("completed_queries", completedCount).
		Msg("Query processing completed")

	return nil
}

// processActiveOperationsMetadata, metadata'daki active operations JSON'ını işler ve InfluxDB'ye yazar
func (s *Server) processActiveOperationsMetadata(ctx context.Context, agentID, activeOperationsJSON string) error {
	// JSON'ı parse et
	var operations []map[string]interface{}
	if err := json.Unmarshal([]byte(activeOperationsJSON), &operations); err != nil {
		return fmt.Errorf("active operations JSON parse hatası: %v", err)
	}

	logger.Debug().
		Str("agent_id", agentID).
		Int("operation_count", len(operations)).
		Str("json_preview", activeOperationsJSON[:min(200, len(activeOperationsJSON))]).
		Msg("Active operations metadata parse edildi")

	// Her operation için InfluxDB point'i oluştur
	activeCount := 0

	for i, operationData := range operations {
		// Agent'dan gelen timestamp'ı kullan, yoksa current time
		timestamp := time.Now()
		if timestampStr, ok := operationData["timestamp"].(string); ok && timestampStr != "" {
			if parsedTime, err := time.Parse(time.RFC3339Nano, timestampStr); err == nil {
				timestamp = parsedTime
			}
		}

		// Debug: field'ları ve tiplerini logla
		logger.Debug().
			Str("agent_id", agentID).
			Int("operation_index", i).
			Interface("operation_data", operationData).
			Msg("Operation data debug")

		activeCount++
		// Active operation'ları InfluxDB'ye yaz
		if err := s.writeActiveOperationToInfluxDB(ctx, agentID, operationData, timestamp); err != nil {
			logger.Error().
				Str("agent_id", agentID).
				Err(err).
				Msg("Active operation InfluxDB yazma hatası")
			return err
		}
	}

	logger.Debug().
		Str("agent_id", agentID).
		Int("active_operations", activeCount).
		Msg("Operation processing completed")

	return nil
}

// writeActiveQueryToInfluxDB, tek bir active query'yi InfluxDB'ye yazar
// TTL: 10 dakika (otomatik expire)
func (s *Server) writeActiveQueryToInfluxDB(ctx context.Context, agentID string, queryData map[string]interface{}, timestamp time.Time) error {
	// Tags'leri oluştur
	tags := map[string]string{
		"agent_id": agentID,
	}

	// Query-specific tags ekle
	if dbName, ok := queryData["database_name"].(string); ok {
		tags["database"] = dbName
	}
	if username, ok := queryData["username"].(string); ok {
		tags["username"] = username
	}
	if appName, ok := queryData["application_name"].(string); ok {
		tags["application"] = appName
	}
	if state, ok := queryData["state"].(string); ok {
		tags["state"] = state
	}
	if clientAddr, ok := queryData["client_addr"].(string); ok && clientAddr != "" {
		tags["client_addr"] = clientAddr
	}
	if waitEventType, ok := queryData["wait_event_type"].(string); ok && waitEventType != "" {
		tags["wait_event_type"] = waitEventType
	}
	if waitEvent, ok := queryData["wait_event"].(string); ok && waitEvent != "" {
		tags["wait_event"] = waitEvent
	}

	// Fields'leri oluştur
	fields := make(map[string]interface{})

	// PID field'ı ekle
	if pid, ok := queryData["pid"]; ok {
		switch v := pid.(type) {
		case float64:
			fields["pid"] = int64(v)
		case float32:
			fields["pid"] = int64(v)
		case int:
			fields["pid"] = int64(v)
		case int64:
			fields["pid"] = v
		case string:
			if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
				fields["pid"] = parsed
			}
		}
	}

	// Duration field'ı - debug ile birlikte
	if duration, ok := queryData["duration_seconds"]; ok {
		logger.Debug().
			Str("agent_id", agentID).
			Interface("duration_raw", duration).
			Str("duration_type", fmt.Sprintf("%T", duration)).
			Msg("Duration field debug")

		// Farklı tip dönüşümlerini dene
		switch v := duration.(type) {
		case float64:
			fields["duration"] = v
		case float32:
			fields["duration"] = float64(v)
		case int:
			fields["duration"] = float64(v)
		case int64:
			fields["duration"] = float64(v)
		case string:
			if parsed, err := strconv.ParseFloat(v, 64); err == nil {
				fields["duration"] = parsed
			}
		}
	}

	// Query text field'ı - debug ile birlikte
	if queryText, ok := queryData["query"].(string); ok {
		fields["text"] = queryText
		logger.Debug().
			Str("agent_id", agentID).
			Str("query_text", queryText).
			Msg("Query text field debug")
	} else {
		logger.Debug().
			Str("agent_id", agentID).
			Interface("query_raw", queryData["query"]).
			Str("query_type", fmt.Sprintf("%T", queryData["query"])).
			Msg("Query text field missing or wrong type")
	}

	// Query start time field'ı ekle
	if queryStartStr, ok := queryData["query_start"].(string); ok && queryStartStr != "" {
		if queryStart, err := time.Parse(time.RFC3339Nano, queryStartStr); err == nil {
			fields["query_start"] = queryStart
		}
	}

	logger.Debug().
		Str("agent_id", agentID).
		Interface("tags", tags).
		Interface("fields", fields).
		Msg("Final active query InfluxDB write data")

	// InfluxDB'ye yaz
	return s.influxWriter.WriteMetric(ctx, "postgresql_active_query", tags, fields, timestamp)
}

// writeCompletedQueryToInfluxDB, tamamlanmış query'leri InfluxDB'ye yazar
// TTL: Yok (kalıcı history)
func (s *Server) writeCompletedQueryToInfluxDB(ctx context.Context, agentID string, queryData map[string]interface{}, timestamp time.Time) error {
	// Tags'leri oluştur
	tags := map[string]string{
		"agent_id":    agentID,
		"query_state": "completed",
	}

	// Query-specific tags ekle
	if dbName, ok := queryData["database_name"].(string); ok {
		tags["database"] = dbName
	}
	if username, ok := queryData["username"].(string); ok {
		tags["username"] = username
	}
	if appName, ok := queryData["application_name"].(string); ok {
		tags["application"] = appName
	}
	if clientAddr, ok := queryData["client_addr"].(string); ok && clientAddr != "" {
		tags["client_addr"] = clientAddr
	}
	if waitEventType, ok := queryData["wait_event_type"].(string); ok && waitEventType != "" {
		tags["wait_event_type"] = waitEventType
	}
	if waitEvent, ok := queryData["wait_event"].(string); ok && waitEvent != "" {
		tags["wait_event"] = waitEvent
	}

	// Fields'leri oluştur
	fields := make(map[string]interface{})

	// Query text field'ı
	if queryText, ok := queryData["query"].(string); ok {
		fields["query_text"] = queryText
	}

	// Duration field'ı
	if duration, ok := queryData["duration_seconds"]; ok {
		switch v := duration.(type) {
		case float64:
			fields["duration_seconds"] = v
		case float32:
			fields["duration_seconds"] = float64(v)
		case int:
			fields["duration_seconds"] = float64(v)
		case int64:
			fields["duration_seconds"] = float64(v)
		case string:
			if parsed, err := strconv.ParseFloat(v, 64); err == nil {
				fields["duration_seconds"] = parsed
			}
		}
	}

	// PID field'ı
	if pid, ok := queryData["pid"]; ok {
		switch v := pid.(type) {
		case float64:
			fields["pid"] = int64(v)
		case float32:
			fields["pid"] = int64(v)
		case int:
			fields["pid"] = int64(v)
		case int64:
			fields["pid"] = v
		case string:
			if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
				fields["pid"] = parsed
			}
		}
	}

	// Completion time - agent'dan gelen completion_time'ı kullan
	if completionTimeStr, ok := queryData["completion_time"].(string); ok && completionTimeStr != "" {
		if completionTime, err := time.Parse(time.RFC3339Nano, completionTimeStr); err == nil {
			fields["completion_time"] = completionTime
		} else {
			// Parse edilemezse current time kullan
			fields["completion_time"] = timestamp
		}
	} else {
		// completion_time yoksa current time kullan
		fields["completion_time"] = timestamp
	}

	// Query start time field'ı ekle
	if queryStartStr, ok := queryData["query_start"].(string); ok && queryStartStr != "" {
		if queryStart, err := time.Parse(time.RFC3339Nano, queryStartStr); err == nil {
			fields["query_start"] = queryStart
		}
	}

	logger.Debug().
		Str("agent_id", agentID).
		Interface("tags", tags).
		Interface("fields", fields).
		Msg("Final completed query InfluxDB write data")

	// InfluxDB'ye yaz
	return s.influxWriter.WriteMetric(ctx, "postgresql_query_history", tags, fields, timestamp)
}

// writeActiveOperationToInfluxDB, tek bir active operation'ı InfluxDB'ye yazar
// TTL: 10 dakika (otomatik expire)
func (s *Server) writeActiveOperationToInfluxDB(ctx context.Context, agentID string, operationData map[string]interface{}, timestamp time.Time) error {
	// Tags'leri oluştur
	tags := map[string]string{
		"agent_id": agentID,
	}

	// Operation-specific tags ekle
	if dbName, ok := operationData["database"].(string); ok {
		tags["database"] = dbName
	}
	if collection, ok := operationData["collection"].(string); ok {
		tags["collection"] = collection
	}
	if opType, ok := operationData["op_type"].(string); ok {
		tags["op_type"] = opType
	}
	if namespace, ok := operationData["namespace"].(string); ok {
		tags["namespace"] = namespace
	}
	if client, ok := operationData["client"].(string); ok && client != "" {
		tags["client"] = client
	}
	if lockType, ok := operationData["lock_type"].(string); ok && lockType != "" {
		tags["lock_type"] = lockType
	}

	// Fields'leri oluştur
	fields := make(map[string]interface{})

	// Operation ID field'ı ekle
	if operationID, ok := operationData["operation_id"].(string); ok {
		fields["operation_id"] = operationID
	}

	// Connection ID field'ı
	if connID, ok := operationData["connection_id"]; ok {
		switch v := connID.(type) {
		case float64:
			fields["connection_id"] = int64(v)
		case float32:
			fields["connection_id"] = int64(v)
		case int:
			fields["connection_id"] = int64(v)
		case int64:
			fields["connection_id"] = v
		case string:
			if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
				fields["connection_id"] = parsed
			}
		}
	}

	// Duration field'ı
	if duration, ok := operationData["duration_seconds"]; ok {
		logger.Debug().
			Str("agent_id", agentID).
			Interface("duration_raw", duration).
			Msg("Processing MongoDB operation duration")

		switch v := duration.(type) {
		case float64:
			fields["duration"] = v
		case float32:
			fields["duration"] = float64(v)
		case int:
			fields["duration"] = float64(v)
		case int64:
			fields["duration"] = float64(v)
		case string:
			if parsed, err := strconv.ParseFloat(v, 64); err == nil {
				fields["duration"] = parsed
			}
		}
	}

	// Command field'ı
	if command, ok := operationData["command"].(string); ok {
		fields["command"] = command
	}

	// Command details field'ı - JSON olarak sakla
	if commandDetails, ok := operationData["command_details"]; ok {
		if commandDetailsJSON, err := json.Marshal(commandDetails); err == nil {
			fields["command_details"] = string(commandDetailsJSON)
		} else {
			logger.Debug().
				Str("agent_id", agentID).
				Err(err).
				Interface("command_details", commandDetails).
				Msg("Command details JSON marshal hatası")
		}
	}

	// 🆕 Yeni truncated command alanları
	if commandTruncated, ok := operationData["command_truncated"].(bool); ok {
		fields["command_truncated"] = commandTruncated
	}
	if commandType, ok := operationData["command_type"].(string); ok {
		fields["command_type"] = commandType
	}
	if truncatedContent, ok := operationData["truncated_content"]; ok {
		if truncatedContentJSON, err := json.Marshal(truncatedContent); err == nil {
			fields["truncated_content"] = string(truncatedContentJSON)
		} else {
			logger.Debug().
				Str("agent_id", agentID).
				Err(err).
				Interface("truncated_content", truncatedContent).
				Msg("Truncated content JSON marshal hatası")
		}
	}
	if inferredCommandType, ok := operationData["inferred_command_type"].(string); ok {
		fields["inferred_command_type"] = inferredCommandType
	}
	if planSummary, ok := operationData["plan_summary"].(string); ok {
		fields["plan_summary"] = planSummary
	}
	if note, ok := operationData["note"].(string); ok {
		fields["note"] = note
	}

	// 🆕 Client details field'ı - JSON olarak sakla
	if clientDetails, ok := operationData["client_details"]; ok {
		if clientDetailsJSON, err := json.Marshal(clientDetails); err == nil {
			fields["client_details"] = string(clientDetailsJSON)
		} else {
			logger.Debug().
				Str("agent_id", agentID).
				Err(err).
				Interface("client_details", clientDetails).
				Msg("Client details JSON marshal hatası")
		}
	}

	// Thread ID field'ı
	if threadID, ok := operationData["thread_id"].(string); ok {
		fields["thread_id"] = threadID
	}

	// Waiting for lock field'ı
	if waitingForLock, ok := operationData["waiting_for_lock"].(bool); ok {
		fields["waiting_for_lock"] = waitingForLock
	}

	logger.Debug().
		Str("agent_id", agentID).
		Interface("tags", tags).
		Interface("fields", fields).
		Msg("Final active operation InfluxDB write data")

	// InfluxDB'ye yaz - TTL ile (10 dakika)
	return s.influxWriter.WriteMetric(ctx, "mongodb_active_operation", tags, fields, timestamp)
}

// writeMetricToInfluxDB tek bir metric'i InfluxDB'ye yazar
func (s *Server) writeMetricToInfluxDB(ctx context.Context, batch *pb.MetricBatch, metric *pb.Metric) error {
	// Metric değerini float64'e çevir
	value := s.getMetricValueAsFloat(metric.Value)

	// Tags'leri map'e çevir
	tags := make(map[string]string)
	tags["agent_id"] = batch.AgentId
	tags["metric_type"] = batch.MetricType

	for _, tag := range metric.Tags {
		tags[tag.Key] = tag.Value
	}

	// Fields'leri oluştur
	fieldName := s.extractFieldName(metric.Name)

	fields := map[string]interface{}{
		fieldName: value,
	}

	if metric.Unit != "" {
		fields[fieldName+"_unit"] = metric.Unit
	}
	if metric.Description != "" {
		fields[fieldName+"_description"] = metric.Description
	}

	// Timestamp'i time.Time'a çevir
	timestamp := time.Unix(0, metric.Timestamp)

	// Measurement adını metric adından çıkar
	measurement := s.extractMeasurementName(metric.Name)

	// InfluxDB'ye yaz
	return s.influxWriter.WriteMetric(ctx, measurement, tags, fields, timestamp)
}

// writeBatchMetricsToInfluxDB batch'teki tüm metrikler için tek seferde InfluxDB'ye yazar
func (s *Server) writeBatchMetricsToInfluxDB(ctx context.Context, batch *pb.MetricBatch) error {
	if s.influxWriter == nil {
		return nil
	}

	// Tüm metrikler için points oluştur
	var points []*write.Point
	
	for _, metric := range batch.Metrics {
		// Tags'leri map'e çevir
		tags := make(map[string]string)
		tags["agent_id"] = batch.AgentId
		tags["metric_type"] = batch.MetricType
		
		for _, tag := range metric.Tags {
			tags[tag.Key] = tag.Value
		}
		
		// String değerleri tag olarak ekle, numeric değerleri field olarak
		fieldName := s.extractFieldName(metric.Name)
		fields := make(map[string]interface{})
		
		if metric.Value.GetStringValue() != "" {
			// String değerleri tag olarak ekle ve dummy field ekle
			tags[fieldName] = metric.Value.GetStringValue()
			fields["value"] = 1 // InfluxDB requires at least one field
		} else {
			// Numeric değerleri field olarak ekle
			value := s.getMetricValueAsFloat(metric.Value)
			fields[fieldName] = value
		}
		
		if metric.Unit != "" {
			fields[fieldName+"_unit"] = metric.Unit
		}
		if metric.Description != "" {
			fields[fieldName+"_description"] = metric.Description
		}
		
		// Timestamp'i time.Time'a çevir
		timestamp := time.Unix(0, metric.Timestamp)
		
		// Measurement adını metric adından çıkar
		measurement := s.extractMeasurementName(metric.Name)
		
		// Point oluştur
		point := influxdb2.NewPointWithMeasurement(measurement)
		
		// Tags ekle
		for k, v := range tags {
			point = point.AddTag(k, v)
		}
		
		// Fields ekle
		for k, v := range fields {
			point = point.AddField(k, v)
		}
		
		// Timestamp ayarla
		point = point.SetTime(timestamp)
		
		points = append(points, point)
	}
	
	// Batch olarak InfluxDB'ye yaz
	return s.influxWriter.WriteBatchPoints(ctx, points...)
}

// getMetricValueAsFloat MetricValue'yu float64'e çevirir
func (s *Server) getMetricValueAsFloat(value *pb.MetricValue) float64 {
	if value == nil {
		return 0
	}

	switch v := value.Value.(type) {
	case *pb.MetricValue_DoubleValue:
		return v.DoubleValue
	case *pb.MetricValue_IntValue:
		return float64(v.IntValue)
	case *pb.MetricValue_BoolValue:
		if v.BoolValue {
			return 1
		}
		return 0
	case *pb.MetricValue_StringValue:
		// String değerleri parse etmeye çalış
		if parsed, err := strconv.ParseFloat(v.StringValue, 64); err == nil {
			return parsed
		}
		return 0
	default:
		return 0
	}
}

// extractFieldName metric adından field adını çıkarır
func (s *Server) extractFieldName(metricName string) string {
	parts := strings.Split(metricName, ".")
	if len(parts) >= 3 {
		return parts[2]
	}
	if len(parts) >= 2 {
		return parts[len(parts)-1]
	}
	return "value"
}

// extractMeasurementName metric adından measurement adını çıkarır
func (s *Server) extractMeasurementName(metricName string) string {
	// "mongodb.operations.insert" -> "mongodb_operations"
	// "postgresql.connections.active" -> "postgresql_connections"
	// "mssql.performance.cpu" -> "mssql_performance"
	// "system.cpu.usage" -> "system_cpu"

	parts := strings.Split(metricName, ".")
	if len(parts) >= 2 {
		return strings.Join(parts[:2], "_")
	}

	// Fallback: ilk part'ı kullan
	if len(parts) > 0 {
		return parts[0]
	}

	return "unknown"
}

// CollectMetrics, agent'a metric toplama talebi gönderir
func (s *Server) CollectMetrics(ctx context.Context, req *pb.CollectMetricsRequest) (*pb.CollectMetricsResponse, error) {
	logger.Info().
		Str("agent_id", req.AgentId).
		Strs("metric_types", req.MetricTypes).
		Msg("Metric toplama talebi")

	// Bu metod şu anda sadece acknowledgment döndürüyor
	// Gelecekte agent'a aktif olarak metric toplama talebi göndermek için kullanılabilir

	return &pb.CollectMetricsResponse{
		Status:  "success",
		Message: "Metric collection request acknowledged",
	}, nil
}

// ReportAlarm, agent'lardan gelen alarm bildirimlerini işler
func (s *Server) ReportAlarm(ctx context.Context, req *pb.ReportAlarmRequest) (*pb.ReportAlarmResponse, error) {
	logger.Info().
		Str("agent_id", req.AgentId).
		Int("alarm_count", len(req.Events)).
		Msg("ReportAlarm metodu çağrıldı")

	// Agent ID doğrula
	s.mu.RLock()
	_, agentExists := s.agents[req.AgentId]
	s.mu.RUnlock()

	if !agentExists {
		logger.Warn().Str("agent_id", req.AgentId).Msg("Bilinmeyen agent'dan alarm bildirimi")
		// Bilinmeyen agent olsa da işlemeye devam ediyoruz
	}

	// Gelen her alarmı işle
	for _, event := range req.Events {
		logger.Info().
			Str("event_id", event.Id).
			Str("status", event.Status).
			Str("metric_name", event.MetricName).
			Str("metric_value", event.MetricValue).
			Str("severity", event.Severity).
			Msg("Alarm işleniyor")

		// Alarm verilerini veritabanına kaydet
		err := s.saveAlarmToDatabase(ctx, event)
		if err != nil {
			logger.Error().Err(err).Str("event_id", event.Id).Msg("Alarm veritabanına kaydedilemedi")
			// Devam et, bir alarmın kaydedilememesi diğerlerini etkilememeli
		}

		// Bildirimi gönder (Slack, Email vb.)
		err = s.sendAlarmNotification(ctx, event)
		if err != nil {
			logger.Error().Err(err).Str("event_id", event.Id).Msg("Alarm bildirimi gönderilemedi")
			// Devam et, bir bildirimin gönderilememesi diğerlerini etkilememeli
		}
	}

	return &pb.ReportAlarmResponse{
		Status: "success",
	}, nil
}

// saveAlarmToDatabase, alarm olayını veritabanına kaydeder
func (s *Server) saveAlarmToDatabase(ctx context.Context, event *pb.AlarmEvent) error {
	logger.Debug().Str("alarm_id", event.AlarmId).Msg("Starting saveAlarmToDatabase")

	// Veritabanı bağlantısını kontrol et
	if err := s.checkDatabaseConnection(); err != nil {
		logger.Error().Err(err).Str("alarm_id", event.AlarmId).Msg("Database connection check failed")
		return fmt.Errorf("veritabanı bağlantı hatası: %v", err)
	}
	logger.Debug().Str("alarm_id", event.AlarmId).Msg("Database connection check passed")

	// SQL sorgusu hazırla
	query := `
		INSERT INTO alarms (
			alarm_id, 
			event_id, 
			agent_id, 
			status, 
			metric_name, 
			metric_value, 
			message, 
			severity,
			created_at,
			database
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	// Zaman damgasını parse et
	timestamp, err := time.Parse(time.RFC3339, event.Timestamp)
	if err != nil {
		logger.Warn().
			Str("timestamp", event.Timestamp).
			Err(err).
			Str("alarm_id", event.AlarmId).
			Msg("Failed to parse timestamp, using current time")
		timestamp = time.Now() // Parse edilemezse şu anki zamanı kullan
	} else {
		logger.Debug().
			Time("timestamp", timestamp).
			Str("alarm_id", event.AlarmId).
			Msg("Successfully parsed timestamp")
	}

	// UTC zamanını Türkiye saatine (UTC+3) çevir
	turkeyLoc, err := time.LoadLocation("Europe/Istanbul")
	if err != nil {
		logger.Warn().Err(err).Str("alarm_id", event.AlarmId).Msg("Failed to load Turkey timezone, using UTC")
	} else {
		timestamp = timestamp.In(turkeyLoc)
		logger.Debug().
			Time("timestamp", timestamp).
			Str("alarm_id", event.AlarmId).
			Msg("Converted timestamp to Turkey time")
	}

	// Veritabanına kaydet
	if event.MetricName == "postgresql_slow_queries" {
		logger.Debug().
			Str("agent_id", event.AgentId).
			Str("metric_value", event.MetricValue).
			Str("message", event.Message).
			Str("alarm_id", event.AlarmId).
			Msg("Slow query alarm received")

		// postgresql_slow_queries için database alanı boşsa varsayılan olarak "postgres" ata
		if event.Database == "" {
			logger.Debug().Str("alarm_id", event.AlarmId).Msg("Setting default database 'postgres' for postgresql_slow_queries alarm")
			event.Database = "postgres"
		}
	}

	logger.Debug().
		Str("alarm_id", event.AlarmId).
		Str("event_id", event.Id).
		Msg("Executing database insert")
	_, err = s.db.ExecContext(
		ctx,
		query,
		event.AlarmId,
		event.Id,
		event.AgentId,
		event.Status,
		event.MetricName,
		event.MetricValue,
		event.Message,
		event.Severity,
		timestamp,
		event.Database,
	)

	if err != nil {
		logger.Error().
			Err(err).
			Str("alarm_id", event.AlarmId).
			Str("event_id", event.Id).
			Msg("Failed to save alarm to database")
		return fmt.Errorf("alarm veritabanına kaydedilemedi: %v", err)
	}

	logger.Info().
		Str("event_id", event.Id).
		Str("metric_name", event.MetricName).
		Str("agent_id", event.AgentId).
		Str("alarm_id", event.AlarmId).
		Msg("Successfully saved alarm to database")
	return nil
}

// sendAlarmNotification, alarm olayını ilgili kanallara bildirir
func (s *Server) sendAlarmNotification(ctx context.Context, event *pb.AlarmEvent) error {
	// Notification ayarlarını veritabanından al
	var slackWebhookURL string
	var slackEnabled bool
	var emailEnabled bool
	var emailServer, emailPort, emailUser, emailPassword, emailFrom string
	var emailRecipientsStr string

	query := `
		SELECT 
			slack_webhook_url,
			slack_enabled,
			email_enabled,
			email_server,
			email_port,
			email_user,
			email_password,
			email_from,
			email_recipients
		FROM notification_settings
		ORDER BY id DESC
		LIMIT 1
	`

	err := s.db.QueryRowContext(ctx, query).Scan(
		&slackWebhookURL,
		&slackEnabled,
		&emailEnabled,
		&emailServer,
		&emailPort,
		&emailUser,
		&emailPassword,
		&emailFrom,
		&emailRecipientsStr,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			logger.Debug().Str("alarm_id", event.AlarmId).Msg("Notification ayarları bulunamadı, bildirim gönderilemiyor")
			return nil // Ayar yok, hata kabul etmiyoruz
		}
		return fmt.Errorf("notification ayarları alınamadı: %v", err)
	}

	// Slack bildirimi gönder
	if slackEnabled && slackWebhookURL != "" {
		err = s.sendSlackNotification(event, slackWebhookURL)
		if err != nil {
			logger.Error().Err(err).Str("alarm_id", event.AlarmId).Msg("Slack bildirimi gönderilemedi")
		}
	}

	// Email bildirimi gönder
	if emailEnabled && emailServer != "" && emailFrom != "" && emailRecipientsStr != "" {
		// PostgreSQL array formatını parse et
		var emailRecipients []string
		if len(emailRecipientsStr) > 2 { // En az {} olmalı
			// PostgreSQL array formatı: {email1,email2,...}
			trimmedStr := emailRecipientsStr[1 : len(emailRecipientsStr)-1] // Başındaki { ve sonundaki } karakterlerini kaldır
			if trimmedStr != "" {
				emailRecipients = strings.Split(trimmedStr, ",")
			}
		}

		if len(emailRecipients) > 0 {
			err = s.sendEmailNotification(event, emailServer, emailPort, emailUser, emailPassword, emailFrom, emailRecipients)
			if err != nil {
				logger.Error().Err(err).Str("alarm_id", event.AlarmId).Msg("Email bildirimi gönderilemedi")
			}
		}
	}

	return nil
}

// sendSlackNotification, Slack webhook'u aracılığıyla bildirim gönderir
func (s *Server) sendSlackNotification(event *pb.AlarmEvent, webhookURL string) error {
	// Alarm durumuna göre emoji ve renk belirle
	var emoji, color string
	if event.Status == "triggered" {
		if event.Severity == "critical" {
			emoji = ":red_circle:"
			color = "#FF0000" // Kırmızı
		} else if event.Severity == "warning" {
			emoji = ":warning:"
			color = "#FFA500" // Turuncu
		} else {
			emoji = ":information_source:"
			color = "#0000FF" // Mavi
		}
	} else if event.Status == "resolved" {
		emoji = ":white_check_mark:"
		color = "#00FF00" // Yeşil
	} else {
		emoji = ":grey_question:"
		color = "#808080" // Gri
	}

	// Mesaj içeriği
	title := fmt.Sprintf("%s Alarm: %s", strings.ToUpper(event.Status), event.MetricName)

	// JSON mesajı oluştur
	message := map[string]interface{}{
		"attachments": []map[string]interface{}{
			{
				"fallback":    fmt.Sprintf("%s %s: %s - %s", emoji, title, event.MetricValue, event.Message),
				"color":       color,
				"title":       title,
				"title_link":  "http://clustereye.io/alarms", // Alarmlar sayfasına yönlendir
				"text":        event.Message,
				"footer":      fmt.Sprintf("Agent: %s | Alarm ID: %s", event.AgentId, event.AlarmId),
				"footer_icon": "https://clustereye.io/favicon.ico", // Varsa logo URL'si
				"ts":          time.Now().Unix(),
				"fields": []map[string]interface{}{
					{
						"title": "Metric",
						"value": event.MetricName,
						"short": true,
					},
					{
						"title": "Value",
						"value": event.MetricValue,
						"short": true,
					},
					{
						"title": "Severity",
						"value": event.Severity,
						"short": true,
					},
					{
						"title": "Status",
						"value": event.Status,
						"short": true,
					},
					{
						"title": "Database",
						"value": func() string {
							if event.Database != "" {
								return event.Database
							}
							return "N/A"
						}(),
						"short": true,
					},
				},
			},
		},
	}

	// JSON'a dönüştür
	jsonMessage, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("JSON dönüşüm hatası: %v", err)
	}

	// Slack'e gönder
	resp, err := http.Post(webhookURL, "application/json", bytes.NewBuffer(jsonMessage))
	if err != nil {
		return fmt.Errorf("HTTP POST hatası: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack yanıt kodu başarısız: %d", resp.StatusCode)
	}

	logger.Info().Str("alarm_id", event.Id).Msg("Slack bildirimi başarıyla gönderildi")
	return nil
}

// sendEmailNotification, email aracılığıyla bildirim gönderir
func (s *Server) sendEmailNotification(event *pb.AlarmEvent, server, port, user, password, from string, recipients []string) error {
	// Alarm durumuna göre konu oluştur
	var subject string
	if event.Status == "triggered" {
		if event.Severity == "critical" {
			subject = fmt.Sprintf("[KRITIK ALARM] %s: %s", event.MetricName, event.MetricValue)
		} else if event.Severity == "warning" {
			subject = fmt.Sprintf("[UYARI] %s: %s", event.MetricName, event.MetricValue)
		} else {
			subject = fmt.Sprintf("[BILGI] %s: %s", event.MetricName, event.MetricValue)
		}
	} else if event.Status == "resolved" {
		subject = fmt.Sprintf("[COZULDU] %s: %s", event.MetricName, event.MetricValue)
	} else {
		subject = fmt.Sprintf("[DURUM: %s] %s: %s", event.Status, event.MetricName, event.MetricValue)
	}

	// Mesaj içeriğini oluştur
	htmlBody := fmt.Sprintf(`
	<!DOCTYPE html>
	<html>
	<head>
		<style>
			body { font-family: Arial, sans-serif; line-height: 1.6; }
			.header { background-color: #f5f5f5; padding: 20px; border-bottom: 1px solid #ddd; }
			.content { padding: 20px; }
			.footer { background-color: #f5f5f5; padding: 20px; border-top: 1px solid #ddd; font-size: 12px; }
			.alarm-critical { color: #cc0000; }
			.alarm-warning { color: #ff9900; }
			.alarm-info { color: #0066cc; }
			.alarm-resolved { color: #009900; }
			.details { margin-top: 20px; border-top: 1px solid #ddd; padding-top: 20px; }
			.detail-row { display: flex; margin-bottom: 10px; }
			.detail-label { width: 150px; font-weight: bold; }
		</style>
	</head>
	<body>
		<div class="header">
			<h2>ClusterEye Alarm Bildirimi</h2>
		</div>
		<div class="content">
			<h3 class="alarm-%s">%s</h3>
			<p>%s</p>
			
			<div class="details">
				<div class="detail-row">
					<div class="detail-label">Agent ID:</div>
					<div>%s</div>
				</div>
				<div class="detail-row">
					<div class="detail-label">Metrik:</div>
					<div>%s</div>
				</div>
				<div class="detail-row">
					<div class="detail-label">Değer:</div>
					<div>%s</div>
				</div>
				<div class="detail-row">
					<div class="detail-label">Önem Derecesi:</div>
					<div>%s</div>
				</div>
				<div class="detail-row">
					<div class="detail-label">Durum:</div>
					<div>%s</div>
				</div>
				<div class="detail-row">
					<div class="detail-label">Veritabanı:</div>
					<div>%s</div>
				</div>
				<div class="detail-row">
					<div class="detail-label">Alarm ID:</div>
					<div>%s</div>
				</div>
				<div class="detail-row">
					<div class="detail-label">Olay ID:</div>
					<div>%s</div>
				</div>
				<div class="detail-row">
					<div class="detail-label">Zaman:</div>
					<div>%s</div>
				</div>
			</div>
		</div>
		<div class="footer">
			<p>Bu bildirim <a href="https://clustereye.io">ClusterEye</a> tarafından otomatik olarak gönderilmiştir.</p>
		</div>
	</body>
	</html>
	`,
		event.Severity, // CSS sınıfı için
		subject,        // Başlık
		event.Message,  // Mesaj
		event.AgentId,
		event.MetricName,
		event.MetricValue,
		event.Severity,
		event.Status,
		func() string {
			if event.Database != "" {
				return event.Database
			}
			return "N/A"
		}(),
		event.AlarmId,
		event.Id,
		event.Timestamp,
	)

	// Basit metin içeriği
	textBody := fmt.Sprintf("ClusterEye Alarm Bildirimi\n\n%s\n\n%s\n\nAgent: %s\nMetrik: %s\nDeğer: %s\nÖnem: %s\nDurum: %s\nVeritabanı: %s\nAlarm ID: %s\nOlay ID: %s\nZaman: %s",
		subject,
		event.Message,
		event.AgentId,
		event.MetricName,
		event.MetricValue,
		event.Severity,
		event.Status,
		func() string {
			if event.Database != "" {
				return event.Database
			}
			return "N/A"
		}(),
		event.AlarmId,
		event.Id,
		event.Timestamp,
	)

	logger.Info().
		Str("subject", subject).
		Str("alarm_id", event.Id).
		Msg("Email bildirimi gönderiliyor")
	logger.Debug().
		Strs("recipients", recipients).
		Str("alarm_id", event.Id).
		Msg("Email alıcıları")

	// Email gönderme işlemi burada gerçekleştirilecek
	// Bu kısım şimdilik log kaydı yapmaktadır
	// Gerçek SMTP entegrasyonu için aşağıdaki kodu açabilirsiniz:

	/*
		// SMTP sunucusuna bağlan
		smtpAddr := fmt.Sprintf("%s:%s", server, port)

		// SMTP kimlik doğrulama
		auth := smtp.PlainAuth("", user, password, server)

		// Email içeriği
		mime := "MIME-version: 1.0;\nContent-Type: text/html; charset=\"UTF-8\";\n\n"
		msg := []byte("To: " + strings.Join(recipients, ",") + "\r\n" +
			"From: " + from + "\r\n" +
			"Subject: " + subject + "\r\n" +
			mime + "\r\n" +
			htmlBody + "\r\n")

		// Email gönder
		err := smtp.SendMail(smtpAddr, auth, from, recipients, msg)
		if err != nil {
			return fmt.Errorf("email gönderme hatası: %v", err)
		}
	*/

	// Temizlik için değişkenleri kullanıldı olarak işaretle
	_ = htmlBody
	_ = textBody

	// Başarılı bir şekilde gönderildi
	logger.Info().Str("alarm_id", event.Id).Msg("Email bildirimi başarıyla gönderildi (simüle edildi)")
	return nil
}

// SendMongoInfo, agent'dan gelen MongoDB bilgilerini işler
func (s *Server) SendMongoInfo(ctx context.Context, req *pb.MongoInfoRequest) (*pb.MongoInfoResponse, error) {
	logger.Info().Msg("SendMongoInfo metodu çağrıldı")

	// Gelen MongoDB bilgilerini logla
	mongoInfo := req.MongoInfo
	
	// Agent ID'sini metrik verilerinden çıkar ve agent durumunu active yap
	agentID := fmt.Sprintf("agent_%s", mongoInfo.Hostname)
	s.UpdateAgentConnectedStatus(agentID)
	logger.Debug().
		Str("cluster", mongoInfo.ClusterName).
		Str("hostname", mongoInfo.Hostname).
		Str("ip", mongoInfo.Ip).
		Msg("MongoDB bilgileri alındı")

	// Daha detaylı loglama
	logger.Debug().
		Str("cluster", mongoInfo.ClusterName).
		Str("ip", mongoInfo.Ip).
		Str("hostname", mongoInfo.Hostname).
		Str("node_status", mongoInfo.NodeStatus).
		Str("mongo_version", mongoInfo.MongoVersion).
		Str("location", mongoInfo.Location).
		Str("mongo_status", mongoInfo.MongoStatus).
		Str("replica_set_name", mongoInfo.ReplicaSetName).
		Int64("replication_lag_sec", mongoInfo.ReplicationLagSec).
		Str("free_disk", mongoInfo.FreeDisk).
		Int64("fd_percent", int64(mongoInfo.FdPercent)).
		Msg("MongoDB detay bilgileri")

	// Veritabanına kaydetme işlemi
	err := s.saveMongoInfoToDatabase(ctx, mongoInfo)
	if err != nil {
		logger.Error().
			Err(err).
			Str("cluster", mongoInfo.ClusterName).
			Str("hostname", mongoInfo.Hostname).
			Msg("MongoDB bilgileri veritabanına kaydedilemedi")
		return &pb.MongoInfoResponse{
			Status: "error",
		}, nil
	}

	logger.Info().
		Str("cluster", mongoInfo.ClusterName).
		Str("hostname", mongoInfo.Hostname).
		Msg("MongoDB bilgileri başarıyla işlendi ve kaydedildi")

	return &pb.MongoInfoResponse{
		Status: "success",
	}, nil
}

// MongoDB bilgilerini veritabanına kaydetmek için yardımcı fonksiyon
func (s *Server) saveMongoInfoToDatabase(ctx context.Context, mongoInfo *pb.MongoInfo) error {
	// Önce mevcut kaydı kontrol et
	var existingData []byte
	var id int

	checkQuery := `
		SELECT id, jsondata FROM public.mongo_data 
		WHERE clustername = $1 
		ORDER BY id DESC LIMIT 1
	`

	err := s.db.QueryRowContext(ctx, checkQuery, mongoInfo.ClusterName).Scan(&id, &existingData)

	// Yeni node verisi
	mongoData := map[string]interface{}{
		"ClusterName":       mongoInfo.ClusterName,
		"Location":          mongoInfo.Location,
		"FDPercent":         mongoInfo.FdPercent,
		"FreeDisk":          mongoInfo.FreeDisk,
		"Hostname":          mongoInfo.Hostname,
		"IP":                mongoInfo.Ip,
		"NodeStatus":        mongoInfo.NodeStatus,
		"MongoStatus":       mongoInfo.MongoStatus,
		"MongoVersion":      mongoInfo.MongoVersion,
		"ReplicaSetName":    mongoInfo.ReplicaSetName,
		"ReplicationLagSec": mongoInfo.ReplicationLagSec,
		"Port":              mongoInfo.Port,
		"TotalVCPU":         mongoInfo.TotalVcpu,
		"TotalMemory":       mongoInfo.TotalMemory,
		"ConfigPath":        mongoInfo.ConfigPath,
	}

	var jsonData []byte

	// Hata kontrolünü düzgün yap
	if err == nil {
		// Mevcut kayıt var, güncelle
		logger.Debug().
			Str("cluster", mongoInfo.ClusterName).
			Int("id", id).
			Msg("MongoDB cluster için mevcut kayıt bulundu, güncelleniyor")

		var existingJSON map[string][]interface{}
		if err := json.Unmarshal(existingData, &existingJSON); err != nil {
			logger.Error().
				Err(err).
				Str("cluster", mongoInfo.ClusterName).
				Msg("Mevcut MongoDB JSON ayrıştırma hatası")
			return err
		}

		// Cluster array'ini al
		clusterData, ok := existingJSON[mongoInfo.ClusterName]
		if !ok {
			// Eğer cluster verisi yoksa yeni oluştur
			clusterData = []interface{}{}
		}

		// Node'u bul ve güncelle
		nodeFound := false
		nodeChanged := false
		for i, node := range clusterData {
			nodeMap, ok := node.(map[string]interface{})
			if !ok {
				continue
			}

			// Hostname ve IP ile node eşleşmesi kontrol et
			if nodeMap["Hostname"] == mongoInfo.Hostname && nodeMap["IP"] == mongoInfo.Ip {
				// Sadece değişen alanları güncelle
				nodeFound = true

				// Değişiklikleri takip et
				for key, newValue := range mongoData {
					currentValue, exists := nodeMap[key]
					var hasChanged bool

					if !exists {
						// Değer mevcut değil, yeni alan ekleniyor
						hasChanged = true
						logger.Debug().
							Str("hostname", mongoInfo.Hostname).
							Str("field", key).
							Msg("MongoDB node'da yeni alan eklendi")
					} else {
						// Mevcut değer ile yeni değeri karşılaştır
						// Numeric değerler için özel karşılaştırma yap
						hasChanged = !compareValues(currentValue, newValue)
						if hasChanged {
							logger.Debug().
								Str("hostname", mongoInfo.Hostname).
								Str("field", key).
								Interface("old_value", currentValue).
								Interface("new_value", newValue).
								Msg("MongoDB node'da değişiklik tespit edildi")
						}
					}

					if hasChanged {
						nodeMap[key] = newValue
						// NodeStatus, MongoStatus, FreeDisk gibi önemli alanlar değiştiyse işaretle
						if key == "NodeStatus" || key == "MongoStatus" || key == "FreeDisk" || key == "ReplicationLagSec" {
							nodeChanged = true
						}
					}
				}

				clusterData[i] = nodeMap
				break
			}
		}

		// Eğer node bulunamadıysa yeni ekle
		if !nodeFound {
			clusterData = append(clusterData, mongoData)
			nodeChanged = true
			logger.Info().
				Str("hostname", mongoInfo.Hostname).
				Str("cluster", mongoInfo.ClusterName).
				Msg("Yeni MongoDB node eklendi")
		}

		// Eğer önemli bir değişiklik yoksa veritabanını güncelleme
		if !nodeChanged {
			logger.Debug().
				Str("hostname", mongoInfo.Hostname).
				Str("cluster", mongoInfo.ClusterName).
				Msg("MongoDB node'da önemli bir değişiklik yok, güncelleme yapılmadı")
			return nil
		}

		existingJSON[mongoInfo.ClusterName] = clusterData

		// JSON'ı güncelle
		jsonData, err = json.Marshal(existingJSON)
		if err != nil {
			logger.Error().
				Err(err).
				Str("cluster", mongoInfo.ClusterName).
				Msg("MongoDB JSON dönüştürme hatası")
			return err
		}

		// Veritabanını güncelle
		updateQuery := `
			UPDATE public.mongo_data 
			SET jsondata = $1, updated_at = CURRENT_TIMESTAMP
			WHERE id = $2
		`

		_, err = s.db.ExecContext(ctx, updateQuery, jsonData, id)
		if err != nil {
			logger.Error().
				Err(err).
				Str("cluster", mongoInfo.ClusterName).
				Int("id", id).
				Msg("MongoDB veritabanı güncelleme hatası")
			return err
		}

		logger.Info().
			Str("hostname", mongoInfo.Hostname).
			Str("cluster", mongoInfo.ClusterName).
			Int("record_id", id).
			Msg("MongoDB node bilgileri başarıyla güncellendi (önemli değişiklik nedeniyle)")
	} else if err == sql.ErrNoRows {
		// Kayıt bulunamadı, yeni kayıt oluştur
		logger.Info().
			Str("cluster", mongoInfo.ClusterName).
			Str("hostname", mongoInfo.Hostname).
			Msg("MongoDB cluster için kayıt bulunamadı, yeni kayıt oluşturuluyor")

		outerJSON := map[string][]interface{}{
			mongoInfo.ClusterName: {mongoData},
		}

		jsonData, err = json.Marshal(outerJSON)
		if err != nil {
			logger.Error().
				Err(err).
				Str("cluster", mongoInfo.ClusterName).
				Msg("MongoDB yeni kayıt JSON dönüştürme hatası")
			return err
		}

		insertQuery := `
			INSERT INTO public.mongo_data (
				jsondata, clustername, created_at, updated_at
			) VALUES ($1, $2, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
			ON CONFLICT (clustername) DO UPDATE SET
				jsondata = EXCLUDED.jsondata,
				updated_at = CURRENT_TIMESTAMP
		`

		_, err = s.db.ExecContext(ctx, insertQuery, jsonData, mongoInfo.ClusterName)
		if err != nil {
			logger.Error().
				Err(err).
				Str("cluster", mongoInfo.ClusterName).
				Str("hostname", mongoInfo.Hostname).
				Msg("MongoDB veritabanı ekleme hatası")
			return err
		}

		logger.Info().
			Str("cluster", mongoInfo.ClusterName).
			Str("hostname", mongoInfo.Hostname).
			Msg("MongoDB node bilgileri başarıyla veritabanına kaydedildi (yeni kayıt)")
	} else {
		// Başka bir veritabanı hatası oluştu
		logger.Error().
			Err(err).
			Str("cluster", mongoInfo.ClusterName).
			Str("hostname", mongoInfo.Hostname).
			Msg("MongoDB cluster kayıt kontrolü sırasında hata")
		return fmt.Errorf("veritabanı kontrol hatası: %v", err)
	}

	return nil
}

// GetStatusMongo, MongoDB veritabanından durum bilgilerini çeker
func (s *Server) GetStatusMongo(ctx context.Context, _ *structpb.Struct) (*structpb.Value, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT json_agg(sub.jsondata) FROM (SELECT jsondata FROM mongo_data ORDER BY id) AS sub")
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Veritabanı sorgusu başarısız: %v", err)
	}
	defer rows.Close()

	var jsonData []byte
	if rows.Next() {
		err := rows.Scan(&jsonData)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Veri okuma hatası: %v", err)
		}
	}

	// JSON verisini structpb.Value'ya dönüştür
	var jsonValue interface{}
	if err := json.Unmarshal(jsonData, &jsonValue); err != nil {
		return nil, status.Errorf(codes.Internal, "JSON ayrıştırma hatası: %v", err)
	}

	value, err := structpb.NewValue(jsonValue)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Veri dönüştürme hatası: %v", err)
	}

	return value, nil
}

// ListMongoLogs, belirtilen agent'tan MongoDB log dosyalarını listeler
func (s *Server) ListMongoLogs(ctx context.Context, req *pb.MongoLogListRequest) (*pb.MongoLogListResponse, error) {
	logger.Info().Msg("ListMongoLogs çağrıldı")

	// Agent ID'yi önce metadata'dan almayı dene
	md, ok := metadata.FromIncomingContext(ctx)
	if ok {
		agentIDValues := md.Get("agent-id")
		if len(agentIDValues) > 0 {
			logger.Debug().
				Str("agent_id", agentIDValues[0]).
				Msg("Metadata'dan agent ID alındı")
			// Metadata'dan gelen agent ID'yi kullan
			agentID := agentIDValues[0]

			// Agent'a istek gönder ve sonucu al
			response, err := s.sendMongoLogListQuery(ctx, agentID)
			if err != nil {
				logger.Error().
					Err(err).
					Str("agent_id", agentID).
					Msg("MongoDB log dosyaları listelenirken hata")

				// Daha açıklayıcı hata mesajları için gRPC status kodlarına dönüştür
				if strings.Contains(err.Error(), "agent bulunamadı") {
					return nil, status.Errorf(codes.NotFound, "Agent bulunamadı veya bağlantı kapalı: %s", agentID)
				} else if err == context.DeadlineExceeded {
					return nil, status.Errorf(codes.DeadlineExceeded, "İstek zaman aşımına uğradı")
				}

				return nil, status.Errorf(codes.Internal, "MongoDB log dosyaları listelenirken bir hata oluştu: %v", err)
			}

			logger.Info().
				Str("agent_id", agentID).
				Int("file_count", len(response.LogFiles)).
				Msg("MongoDB log dosyaları başarıyla listelendi")
			return response, nil
		}
	}

	// Metadata'dan alınamadıysa, context'ten almayı dene
	agentID := ""
	queryCtx, ok := ctx.Value("agent_id").(string)
	if ok && queryCtx != "" {
		agentID = queryCtx
	}

	if agentID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "Agent ID belirtilmedi")
	}

	// Agent'a istek gönder ve sonucu al
	response, err := s.sendMongoLogListQuery(ctx, agentID)
	if err != nil {
		logger.Error().
			Err(err).
			Str("agent_id", agentID).
			Msg("MongoDB log dosyaları listelenirken hata")

		// Daha açıklayıcı hata mesajları için gRPC status kodlarına dönüştür
		if strings.Contains(err.Error(), "agent bulunamadı") {
			return nil, status.Errorf(codes.NotFound, "Agent bulunamadı veya bağlantı kapalı: %s", agentID)
		} else if err == context.DeadlineExceeded {
			return nil, status.Errorf(codes.DeadlineExceeded, "İstek zaman aşımına uğradı")
		}

		return nil, status.Errorf(codes.Internal, "MongoDB log dosyaları listelenirken bir hata oluştu: %v", err)
	}

	logger.Info().
		Str("agent_id", agentID).
		Int("file_count", len(response.LogFiles)).
		Msg("MongoDB log dosyaları başarıyla listelendi")
	return response, nil
}

// AnalyzeMongoLog, belirtilen agent'tan MongoDB log dosyasını analiz etmesini ister
func (s *Server) AnalyzeMongoLog(ctx context.Context, req *pb.MongoLogAnalyzeRequest) (*pb.MongoLogAnalyzeResponse, error) {
	logger.Info().
		Str("log_file_path", req.LogFilePath).
		Bool("log_path_empty", req.LogFilePath == "").
		Int64("threshold_ms", req.SlowQueryThresholdMs).
		Str("agent_id", req.AgentId).
		Msg("AnalyzeMongoLog çağrıldı")

	// Agent ID'yi önce doğrudan istekten al
	agentID := req.AgentId

	// Boşsa metadata'dan almayı dene
	if agentID == "" {
		md, ok := metadata.FromIncomingContext(ctx)
		if ok {
			agentIDValues := md.Get("agent-id")
			if len(agentIDValues) > 0 {
				logger.Debug().
					Str("agent_id", agentIDValues[0]).
					Msg("Metadata'dan agent ID alındı")
				agentID = agentIDValues[0]
			}
		}
	}

	// Hala boşsa context'ten almayı dene
	if agentID == "" {
		queryCtx, ok := ctx.Value("agent_id").(string)
		if ok && queryCtx != "" {
			agentID = queryCtx
		}
	}

	logger.Debug().Str("agent_id", agentID).Msg("Kullanılan agent_id")

	if agentID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "Agent ID belirtilmedi")
	}

	// İstek parametrelerini kontrol et
	if req.LogFilePath == "" {
		return nil, status.Errorf(codes.InvalidArgument, "Log dosya yolu belirtilmedi")
	}

	// Threshold için varsayılan değeri ayarla
	threshold := req.SlowQueryThresholdMs
	if threshold <= 0 {
		threshold = 100 // Varsayılan 100ms
		logger.Debug().
			Int64("default_threshold", threshold).
			Msg("Threshold değeri 0 veya negatif, varsayılan değer kullanılıyor")
	}

	// Agent'a istek gönder ve sonucu al
	response, err := s.sendMongoLogAnalyzeQuery(ctx, agentID, req.LogFilePath, threshold)
	if err != nil {
		logger.Error().
			Err(err).
			Str("agent_id", agentID).
			Str("log_file_path", req.LogFilePath).
			Msg("MongoDB log analizi için hata")

		// Daha açıklayıcı hata mesajları için gRPC status kodlarına dönüştür
		if strings.Contains(err.Error(), "agent bulunamadı") {
			return nil, status.Errorf(codes.NotFound, "Agent bulunamadı veya bağlantı kapalı: %s", agentID)
		} else if err == context.DeadlineExceeded {
			return nil, status.Errorf(codes.DeadlineExceeded, "İstek zaman aşımına uğradı")
		}

		return nil, status.Errorf(codes.Internal, "MongoDB log analizi için bir hata oluştu: %v", err)
	}

	return response, nil
}

// sendMongoLogListQuery, agent'a MongoDB log dosyalarını listelemesi için sorgu gönderir
func (s *Server) sendMongoLogListQuery(ctx context.Context, agentID string) (*pb.MongoLogListResponse, error) {
	// Agent'ın bağlı olup olmadığını kontrol et
	s.mu.RLock()
	agent, agentExists := s.agents[agentID]
	s.mu.RUnlock()

	if !agentExists || agent == nil {
		return nil, fmt.Errorf("agent bulunamadı veya bağlantı kapalı: %s", agentID)
	}

	// MongoDB log dosyalarını listeleyen bir komut oluştur
	command := "list_mongo_logs"
	queryID := fmt.Sprintf("mongo_log_list_%d", time.Now().UnixNano())

	logger.Debug().
		Str("command", command).
		Str("agent_id", agentID).
		Str("query_id", queryID).
		Msg("MongoDB log listesi için komut")

	// Sonuç kanalı oluştur
	resultChan := make(chan *pb.QueryResult, 1)
	s.queryMu.Lock()
	s.queryResult[queryID] = &QueryResponse{
		ResultChan: resultChan,
	}
	s.queryMu.Unlock()

	// Context'in iptal durumunda kaynakları temizle
	defer func() {
		s.queryMu.Lock()
		delete(s.queryResult, queryID)
		s.queryMu.Unlock()
		close(resultChan)
	}()

	// Sorguyu gönder
	if err := agent.Stream.Send(&pb.ServerMessage{
		Payload: &pb.ServerMessage_Query{
			Query: &pb.Query{
				QueryId: queryID,
				Command: command,
			},
		},
	}); err != nil {
		return nil, fmt.Errorf("sorgu gönderilemedi: %v", err)
	}

	logger.Info().
		Str("agent_id", agentID).
		Str("query_id", queryID).
		Msg("MongoDB log dosyaları için sorgu gönderildi")

	// Cevabı bekle
	select {
	case result := <-resultChan:
		// Sonuç geldi
		if result == nil {
			return nil, fmt.Errorf("null sorgu sonucu alındı")
		}

		logger.Debug().
			Str("query_id", queryID).
			Str("type_url", result.Result.TypeUrl).
			Msg("Agent'tan yanıt alındı")

		// Önce struct olarak ayrıştırmayı dene (Agent'ın gönderdiği tipte)
		var resultStruct structpb.Struct
		if err := result.Result.UnmarshalTo(&resultStruct); err != nil {
			logger.Debug().
				Err(err).
				Str("query_id", queryID).
				Msg("Struct ayrıştırma hatası")

			// Struct ayrıştırma başarısız olursa, MongoLogListResponse olarak dene
			var logListResponse pb.MongoLogListResponse
			if err := result.Result.UnmarshalTo(&logListResponse); err != nil {
				logger.Error().
					Err(err).
					Str("query_id", queryID).
					Msg("MongoLogListResponse ayrıştırma hatası")
				return nil, fmt.Errorf("sonuç ayrıştırma hatası: %v", err)
			}

			logger.Debug().
				Int("file_count", len(logListResponse.LogFiles)).
				Str("query_id", queryID).
				Msg("Doğrudan MongoLogListResponse'a başarıyla ayrıştırıldı")
			return &logListResponse, nil
		}

		// Struct'tan MongoLogListResponse oluştur
		logFiles := make([]*pb.MongoLogFile, 0)
		filesValue, ok := resultStruct.Fields["log_files"]
		if ok && filesValue != nil && filesValue.GetListValue() != nil {
			for _, fileValue := range filesValue.GetListValue().Values {
				if fileValue.GetStructValue() != nil {
					fileStruct := fileValue.GetStructValue()

					// Dosya değerlerini al
					nameValue := fileStruct.Fields["name"].GetStringValue()
					pathValue := fileStruct.Fields["path"].GetStringValue()
					sizeValue := int64(fileStruct.Fields["size"].GetNumberValue())
					lastModifiedValue := int64(fileStruct.Fields["last_modified"].GetNumberValue())

					// MongoLogFile oluştur
					logFile := &pb.MongoLogFile{
						Name:         nameValue,
						Path:         pathValue,
						Size:         sizeValue,
						LastModified: lastModifiedValue,
					}

					logFiles = append(logFiles, logFile)
				}
			}
		}

		logger.Debug().
			Int("file_count", len(logFiles)).
			Str("query_id", queryID).
			Msg("Struct'tan oluşturulan log dosyaları sayısı")

		return &pb.MongoLogListResponse{
			LogFiles: logFiles,
		}, nil

	case <-ctx.Done():
		// Context iptal edildi veya zaman aşımına uğradı
		return nil, ctx.Err()
	}
}

// SendMongoDatabaseListQuery, agent'a MongoDB veritabanlarını listelemesi için sorgu gönderir
func (s *Server) SendMongoDatabaseListQuery(ctx context.Context, agentID string) ([]string, error) {
	// Agent'ın bağlı olup olmadığını kontrol et
	s.mu.RLock()
	agent, agentExists := s.agents[agentID]
	s.mu.RUnlock()

	if !agentExists || agent == nil {
		return nil, fmt.Errorf("agent bulunamadı veya bağlantı kapalı: %s", agentID)
	}

	// MongoDB database listesi için bir komut oluştur
	command := "list_mongo_databases"
	queryID := fmt.Sprintf("mongo_db_list_%d", time.Now().UnixNano())

	logger.Debug().
		Str("command", command).
		Str("agent_id", agentID).
		Str("query_id", queryID).
		Msg("MongoDB database listesi için komut")

	// Sonuç kanalı oluştur
	resultChan := make(chan *pb.QueryResult, 1)
	s.queryMu.Lock()
	s.queryResult[queryID] = &QueryResponse{
		ResultChan: resultChan,
	}
	s.queryMu.Unlock()

	// Context'in iptal durumunda kaynakları temizle
	defer func() {
		s.queryMu.Lock()
		delete(s.queryResult, queryID)
		s.queryMu.Unlock()
		close(resultChan)
	}()

	// Sorguyu gönder
	if err := agent.Stream.Send(&pb.ServerMessage{
		Payload: &pb.ServerMessage_Query{
			Query: &pb.Query{
				QueryId: queryID,
				Command: command,
			},
		},
	}); err != nil {
		return nil, fmt.Errorf("sorgu gönderilemedi: %v", err)
	}

	logger.Info().
		Str("agent_id", agentID).
		Str("query_id", queryID).
		Msg("MongoDB database listesi için sorgu gönderildi")

	// Cevabı bekle
	select {
	case result := <-resultChan:
		// Sonuç geldi
		if result == nil {
			return nil, fmt.Errorf("null sorgu sonucu alındı")
		}

		logger.Debug().
			Str("query_id", queryID).
			Str("type_url", result.Result.TypeUrl).
			Msg("Agent'tan yanıt alındı")

		// Struct olarak ayrıştır
		var resultStruct structpb.Struct
		if err := result.Result.UnmarshalTo(&resultStruct); err != nil {
			logger.Error().
				Err(err).
				Str("query_id", queryID).
				Msg("Struct ayrıştırma hatası")
			return nil, fmt.Errorf("sonuç ayrıştırma hatası: %v", err)
		}

		// Debug: Tüm field'ları log'la
		logger.Debug().
			Str("query_id", queryID).
			Interface("fields", resultStruct.Fields).
			Msg("Agent'tan gelen response fields")

		// Status kontrolü
		statusValue, ok := resultStruct.Fields["status"]
		if !ok {
			logger.Error().
				Str("query_id", queryID).
				Msg("Status field bulunamadı")
			return nil, fmt.Errorf("agent hatası: Status field yok")
		}
		
		statusStr := statusValue.GetStringValue()
		logger.Debug().
			Str("query_id", queryID).
			Str("status", statusStr).
			Msg("Agent status")
			
		if statusStr != "success" {
			errorMsg := "Bilinmeyen hata"
			if messageValue, exists := resultStruct.Fields["message"]; exists {
				errorMsg = messageValue.GetStringValue()
			}
			logger.Error().
				Str("query_id", queryID).
				Str("status", statusStr).
				Str("error", errorMsg).
				Msg("Agent başarısız status döndürdü")
			return nil, fmt.Errorf("agent hatası: %s", errorMsg)
		}

		// Database listesini çıkar
		var databases []string
		databasesValue, ok := resultStruct.Fields["databases"]
		if ok && databasesValue != nil && databasesValue.GetListValue() != nil {
			for _, dbValue := range databasesValue.GetListValue().Values {
				if dbValue.GetStructValue() != nil {
					dbStruct := dbValue.GetStructValue()
					if nameField, exists := dbStruct.Fields["name"]; exists {
						databases = append(databases, nameField.GetStringValue())
					}
				}
			}
		}

		logger.Debug().
			Int("database_count", len(databases)).
			Str("query_id", queryID).
			Msg("Struct'tan oluşturulan database listesi")

		return databases, nil

	case <-ctx.Done():
		// Context iptal edildi veya zaman aşımına uğradı
		return nil, ctx.Err()
	}
}

// sendMongoLogAnalyzeQuery, agent'a MongoDB log dosyasını analiz etmesi için sorgu gönderir
func (s *Server) sendMongoLogAnalyzeQuery(ctx context.Context, agentID, logFilePath string, thresholdMs int64) (*pb.MongoLogAnalyzeResponse, error) {
	// Agent'ın bağlı olup olmadığını kontrol et
	s.mu.RLock()
	agent, agentExists := s.agents[agentID]
	s.mu.RUnlock()

	if !agentExists || agent == nil {
		return nil, fmt.Errorf("agent bulunamadı veya bağlantı kapalı: %s", agentID)
	}

	// logFilePath boş mu kontrol et
	if logFilePath == "" {
		return nil, fmt.Errorf("log dosya yolu boş olamaz")
	}

	// MongoDB log analizi için bir komut oluştur
	command := fmt.Sprintf("analyze_mongo_log|%s|%d", logFilePath, thresholdMs)
	queryID := fmt.Sprintf("mongo_log_analyze_%d", time.Now().UnixNano())

	logger.Debug().
		Str("command", command).
		Str("agent_id", agentID).
		Str("query_id", queryID).
		Str("log_file_path", logFilePath).
		Int64("threshold_ms", thresholdMs).
		Msg("MongoDB log analizi için komut")

	// Sonuç kanalı oluştur
	resultChan := make(chan *pb.QueryResult, 1)
	s.queryMu.Lock()
	s.queryResult[queryID] = &QueryResponse{
		ResultChan: resultChan,
	}
	s.queryMu.Unlock()

	// Context'in iptal durumunda kaynakları temizle
	defer func() {
		s.queryMu.Lock()
		delete(s.queryResult, queryID)
		s.queryMu.Unlock()
		close(resultChan)
	}()

	// Sorguyu gönder
	err := agent.Stream.Send(&pb.ServerMessage{
		Payload: &pb.ServerMessage_Query{
			Query: &pb.Query{
				QueryId: queryID,
				Command: command,
			},
		},
	})
	if err != nil {
		logger.Error().
			Err(err).
			Str("agent_id", agentID).
			Str("query_id", queryID).
			Str("log_file_path", logFilePath).
			Msg("MongoDB log analizi sorgusu gönderilemedi")
		return nil, fmt.Errorf("sorgu gönderilemedi: %v", err)
	}

	logger.Info().
		Str("agent_id", agentID).
		Str("log_file_path", logFilePath).
		Str("query_id", queryID).
		Msg("MongoDB log analizi için sorgu gönderildi")

	// Cevabı bekle
	select {
	case result := <-resultChan:
		// Sonuç geldi
		if result == nil {
			logger.Error().
				Str("query_id", queryID).
				Msg("Null sorgu sonucu alındı")
			return nil, fmt.Errorf("null sorgu sonucu alındı")
		}

		logger.Debug().
			Str("query_id", queryID).
			Str("type_url", result.Result.TypeUrl).
			Msg("Agent'tan yanıt alındı")

		// Önce struct olarak ayrıştırmayı dene (Agent'ın gönderdiği tipte)
		var resultStruct structpb.Struct
		if err := result.Result.UnmarshalTo(&resultStruct); err != nil {
			logger.Debug().
				Err(err).
				Str("query_id", queryID).
				Msg("Struct ayrıştırma hatası")

			// Struct ayrıştırma başarısız olursa, MongoLogAnalyzeResponse olarak dene
			var analyzeResponse pb.MongoLogAnalyzeResponse
			if err := result.Result.UnmarshalTo(&analyzeResponse); err != nil {
				logger.Error().
					Err(err).
					Str("query_id", queryID).
					Msg("MongoLogAnalyzeResponse ayrıştırma hatası")
				return nil, fmt.Errorf("sonuç ayrıştırma hatası: %v", err)
			}

			logger.Debug().
				Int("log_entries_count", len(analyzeResponse.LogEntries)).
				Str("query_id", queryID).
				Msg("Doğrudan MongoLogAnalyzeResponse'a başarıyla ayrıştırıldı")
			return &analyzeResponse, nil
		}

		// Sonucun içeriğini logla		// Struct'tan MongoLogAnalyzeResponse oluştur
		logEntries := make([]*pb.MongoLogEntry, 0)
		entriesValue, ok := resultStruct.Fields["log_entries"]
		if ok && entriesValue != nil && entriesValue.GetListValue() != nil {
			for _, entryValue := range entriesValue.GetListValue().Values {
				if entryValue.GetStructValue() != nil {
					entryStruct := entryValue.GetStructValue()

					// Log giriş değerlerini al
					timestamp := int64(entryStruct.Fields["timestamp"].GetNumberValue())
					severity := entryStruct.Fields["severity"].GetStringValue() // String olarak al
					component := entryStruct.Fields["component"].GetStringValue()
					context := entryStruct.Fields["context"].GetStringValue()
					message := entryStruct.Fields["message"].GetStringValue()
					dbName := entryStruct.Fields["db_name"].GetStringValue()
					durationMillis := int64(entryStruct.Fields["duration_millis"].GetNumberValue())
					command := entryStruct.Fields["command"].GetStringValue()
					planSummary := entryStruct.Fields["plan_summary"].GetStringValue()
					namespace := entryStruct.Fields["namespace"].GetStringValue()

					// MongoLogEntry oluştur
					logEntry := &pb.MongoLogEntry{
						Timestamp:      timestamp,
						Severity:       severity, // String olarak kullan
						Component:      component,
						Context:        context,
						Message:        message,
						DbName:         dbName,
						DurationMillis: durationMillis,
						Command:        command,
						PlanSummary:    planSummary,
						Namespace:      namespace,
					}

					logEntries = append(logEntries, logEntry)
				}
			}
		}

		logger.Debug().
			Int("log_entries_count", len(logEntries)).
			Str("query_id", queryID).
			Msg("Struct'tan oluşturulan log girişleri sayısı")

		return &pb.MongoLogAnalyzeResponse{
			LogEntries: logEntries,
		}, nil

	case <-ctx.Done():
		// Context iptal edildi veya zaman aşımına uğradı
		logger.Warn().
			Str("query_id", queryID).
			Msg("Context iptal edildi veya zaman aşımına uğradı")
		return nil, ctx.Err()
	}
}

// sendPostgresLogListQuery, agent'a PostgreSQL log dosyalarını listelemesi için sorgu gönderir
func (s *Server) sendPostgresLogListQuery(ctx context.Context, agentID string) (*pb.PostgresLogListResponse, error) {
	// Agent'ın bağlı olup olmadığını kontrol et
	s.mu.RLock()
	agent, agentExists := s.agents[agentID]
	s.mu.RUnlock()

	if !agentExists || agent == nil {
		return nil, fmt.Errorf("agent bulunamadı veya bağlantı kapalı: %s", agentID)
	}

	// PostgreSQL log dosyalarını listeleyen bir komut oluştur
	command := "list_postgres_logs"
	queryID := fmt.Sprintf("postgres_log_list_%d", time.Now().UnixNano())

	logger.Debug().
		Str("command", command).
		Str("agent_id", agentID).
		Str("query_id", queryID).
		Msg("PostgreSQL log listesi için komut")

	// Sonuç kanalı oluştur
	resultChan := make(chan *pb.QueryResult, 1)
	s.queryMu.Lock()
	s.queryResult[queryID] = &QueryResponse{
		ResultChan: resultChan,
	}
	s.queryMu.Unlock()

	// Context'in iptal durumunda kaynakları temizle
	defer func() {
		s.queryMu.Lock()
		delete(s.queryResult, queryID)
		s.queryMu.Unlock()
		close(resultChan)
	}()

	// Sorguyu gönder
	if err := agent.Stream.Send(&pb.ServerMessage{
		Payload: &pb.ServerMessage_Query{
			Query: &pb.Query{
				QueryId: queryID,
				Command: command,
			},
		},
	}); err != nil {
		return nil, fmt.Errorf("sorgu gönderilemedi: %v", err)
	}

	logger.Info().
		Str("agent_id", agentID).
		Str("query_id", queryID).
		Msg("PostgreSQL log dosyaları için sorgu gönderildi")

	// Cevabı bekle
	select {
	case result := <-resultChan:
		// Sonuç geldi
		if result == nil {
			return nil, fmt.Errorf("null sorgu sonucu alındı")
		}

		logger.Debug().
			Str("query_id", queryID).
			Str("type_url", result.Result.TypeUrl).
			Str("agent_id", agentID).
			Msg("PostgreSQL log list query yanıt alındı")

		// Önce struct olarak ayrıştırmayı dene (Agent'ın gönderdiği tipte)
		var resultStruct structpb.Struct
		if err := result.Result.UnmarshalTo(&resultStruct); err != nil {
			logger.Debug().
				Err(err).
				Str("query_id", queryID).
				Str("agent_id", agentID).
				Msg("PostgreSQL log list struct ayrıştırma hatası")

			// Struct ayrıştırma başarısız olursa, PostgresLogListResponse olarak dene
			var logListResponse pb.PostgresLogListResponse
			if err := result.Result.UnmarshalTo(&logListResponse); err != nil {
				logger.Error().
					Err(err).
					Str("query_id", queryID).
					Msg("PostgresLogListResponse ayrıştırma hatası")
				return nil, fmt.Errorf("sonuç ayrıştırma hatası: %v", err)
			}

			logger.Debug().
				Int("file_count", len(logListResponse.LogFiles)).
				Str("query_id", queryID).
				Msg("Doğrudan PostgresLogListResponse'a başarıyla ayrıştırıldı")
			return &logListResponse, nil
		}

		// Sonucun içeriğini logla
		structBytes, _ := json.Marshal(resultStruct.AsMap())
		logger.Debug().
			Str("query_id", queryID).
			RawJSON("struct_content", structBytes).
			Msg("PostgreSQL log struct içeriği")

		// Struct'tan PostgresLogListResponse oluştur
		logFiles := make([]*pb.PostgresLogFile, 0)
		filesValue, ok := resultStruct.Fields["log_files"]
		if ok && filesValue != nil && filesValue.GetListValue() != nil {
			for _, fileValue := range filesValue.GetListValue().Values {
				if fileValue.GetStructValue() != nil {
					fileStruct := fileValue.GetStructValue()

					// Dosya değerlerini al
					nameValue := fileStruct.Fields["name"].GetStringValue()
					pathValue := fileStruct.Fields["path"].GetStringValue()
					sizeValue := int64(fileStruct.Fields["size"].GetNumberValue())
					lastModifiedValue := int64(fileStruct.Fields["last_modified"].GetNumberValue())

					// PostgresLogFile oluştur
					logFile := &pb.PostgresLogFile{
						Name:         nameValue,
						Path:         pathValue,
						Size:         sizeValue,
						LastModified: lastModifiedValue,
					}

					logFiles = append(logFiles, logFile)
				}
			}
		}

		logger.Debug().
			Int("file_count", len(logFiles)).
			Str("query_id", queryID).
			Msg("Struct'tan oluşturulan log dosyaları sayısı")

		return &pb.PostgresLogListResponse{
			LogFiles: logFiles,
		}, nil

	case <-ctx.Done():
		// Context iptal edildi veya zaman aşımına uğradı
		return nil, ctx.Err()
	}
}

// ListPostgresLogs, belirtilen agent'tan PostgreSQL log dosyalarını listeler
func (s *Server) ListPostgresLogs(ctx context.Context, req *pb.PostgresLogListRequest) (*pb.PostgresLogListResponse, error) {
	logger.Info().Msg("ListPostgresLogs çağrıldı")

	// Agent ID'yi önce metadata'dan almayı dene
	md, ok := metadata.FromIncomingContext(ctx)
	if ok {
		agentIDValues := md.Get("agent-id")
		if len(agentIDValues) > 0 {
			logger.Debug().
				Str("agent_id", agentIDValues[0]).
				Msg("Metadata'dan agent ID alındı")
			// Metadata'dan gelen agent ID'yi kullan
			agentID := agentIDValues[0]

			// Agent'a istek gönder ve sonucu al
			response, err := s.sendPostgresLogListQuery(ctx, agentID)
			if err != nil {
				logger.Error().
					Err(err).
					Str("agent_id", agentID).
					Msg("PostgreSQL log dosyaları listelenirken hata")

				// Daha açıklayıcı hata mesajları için gRPC status kodlarına dönüştür
				if strings.Contains(err.Error(), "agent bulunamadı") {
					return nil, status.Errorf(codes.NotFound, "Agent bulunamadı veya bağlantı kapalı: %s", agentID)
				} else if err == context.DeadlineExceeded {
					return nil, status.Errorf(codes.DeadlineExceeded, "İstek zaman aşımına uğradı")
				}

				return nil, status.Errorf(codes.Internal, "PostgreSQL log dosyaları listelenirken bir hata oluştu: %v", err)
			}

			logger.Info().
				Str("agent_id", agentID).
				Int("file_count", len(response.LogFiles)).
				Msg("PostgreSQL log dosyaları başarıyla listelendi")
			return response, nil
		}
	}

	// Metadata'dan alınamadıysa, context'ten almayı dene
	agentID := ""
	queryCtx, ok := ctx.Value("agent_id").(string)
	if ok && queryCtx != "" {
		agentID = queryCtx
	}

	if agentID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "Agent ID belirtilmedi")
	}

	// Agent'a istek gönder ve sonucu al
	response, err := s.sendPostgresLogListQuery(ctx, agentID)
	if err != nil {
		logger.Error().
			Err(err).
			Str("agent_id", agentID).
			Msg("PostgreSQL log dosyaları listelenirken hata")

		// Daha açıklayıcı hata mesajları için gRPC status kodlarına dönüştür
		if strings.Contains(err.Error(), "agent bulunamadı") {
			return nil, status.Errorf(codes.NotFound, "Agent bulunamadı veya bağlantı kapalı: %s", agentID)
		} else if err == context.DeadlineExceeded {
			return nil, status.Errorf(codes.DeadlineExceeded, "İstek zaman aşımına uğradı")
		}

		return nil, status.Errorf(codes.Internal, "PostgreSQL log dosyaları listelenirken bir hata oluştu: %v", err)
	}

	logger.Info().
		Str("agent_id", agentID).
		Int("file_count", len(response.LogFiles)).
		Msg("PostgreSQL log dosyaları başarıyla listelendi")
	return response, nil
}

// AnalyzePostgresLog, belirtilen PostgreSQL log dosyasını analiz eder
func (s *Server) AnalyzePostgresLog(ctx context.Context, req *pb.PostgresLogAnalyzeRequest) (*pb.PostgresLogAnalyzeResponse, error) {
	// Agent ID'yi context'ten al
	agentID := req.AgentId
	if agentID == "" {
		return nil, fmt.Errorf("agent_id gerekli")
	}

	// Log dosya yolunu kontrol et
	if req.LogFilePath == "" {
		return nil, fmt.Errorf("log_file_path gerekli")
	}

	// Varsayılan threshold değerini ayarla
	thresholdMs := req.SlowQueryThresholdMs
	if thresholdMs <= 0 {
		thresholdMs = 1000 // Varsayılan 1 saniye
	}

	// PostgreSQL log analizi isteğini gönder
	return s.sendPostgresLogAnalyzeQuery(ctx, agentID, req.LogFilePath, thresholdMs)
}

// sendPostgresLogAnalyzeQuery, agent'a PostgreSQL log dosyasını analiz etmesi için sorgu gönderir
func (s *Server) sendPostgresLogAnalyzeQuery(ctx context.Context, agentID, logFilePath string, thresholdMs int64) (*pb.PostgresLogAnalyzeResponse, error) {
	// Agent'ın bağlı olup olmadığını kontrol et
	s.mu.RLock()
	agent, agentExists := s.agents[agentID]
	s.mu.RUnlock()

	if !agentExists || agent == nil {
		return nil, fmt.Errorf("agent bulunamadı veya bağlantı kapalı: %s", agentID)
	}

	// logFilePath boş mu kontrol et
	if logFilePath == "" {
		return nil, fmt.Errorf("log dosya yolu boş olamaz")
	}

	// PostgreSQL log analizi için bir komut oluştur
	command := fmt.Sprintf("analyze_postgres_log|%s|%d", logFilePath, thresholdMs)
	queryID := fmt.Sprintf("postgres_log_analyze_%d", time.Now().UnixNano())

	logger.Debug().
		Str("command", command).
		Str("agent_id", agentID).
		Str("query_id", queryID).
		Str("log_file_path", logFilePath).
		Int64("threshold_ms", thresholdMs).
		Msg("PostgreSQL log analizi için komut")

	// Sonuç kanalı oluştur
	resultChan := make(chan *pb.QueryResult, 1)
	s.queryMu.Lock()
	s.queryResult[queryID] = &QueryResponse{
		ResultChan: resultChan,
	}
	s.queryMu.Unlock()

	// Context'in iptal durumunda kaynakları temizle
	defer func() {
		s.queryMu.Lock()
		delete(s.queryResult, queryID)
		s.queryMu.Unlock()
		close(resultChan)
	}()

	// Sorguyu gönder
	err := agent.Stream.Send(&pb.ServerMessage{
		Payload: &pb.ServerMessage_Query{
			Query: &pb.Query{
				QueryId: queryID,
				Command: command,
			},
		},
	})
	if err != nil {
		logger.Error().
			Err(err).
			Str("agent_id", agentID).
			Str("query_id", queryID).
			Str("log_file_path", logFilePath).
			Msg("PostgreSQL log analizi sorgusu gönderilemedi")
		return nil, fmt.Errorf("sorgu gönderilemedi: %v", err)
	}

	logger.Info().
		Str("agent_id", agentID).
		Str("log_file_path", logFilePath).
		Str("query_id", queryID).
		Msg("PostgreSQL log analizi için sorgu gönderildi")

	// Cevabı bekle
	select {
	case result := <-resultChan:
		// Sonuç geldi
		if result == nil {
			logger.Error().
				Str("query_id", queryID).
				Msg("Null sorgu sonucu alındı")
			return nil, fmt.Errorf("null sorgu sonucu alındı")
		}

		logger.Debug().
			Str("query_id", queryID).
			Str("type_url", result.Result.TypeUrl).
			Msg("Agent'tan yanıt alındı")

		// Önce struct olarak ayrıştırmayı dene (Agent'ın gönderdiği tipte)
		var resultStruct structpb.Struct
		if err := result.Result.UnmarshalTo(&resultStruct); err != nil {
			logger.Debug().
				Err(err).
				Str("query_id", queryID).
				Msg("Struct ayrıştırma hatası")

			// Struct ayrıştırma başarısız olursa, PostgresLogAnalyzeResponse olarak dene
			var analyzeResponse pb.PostgresLogAnalyzeResponse
			if err := result.Result.UnmarshalTo(&analyzeResponse); err != nil {
				logger.Error().
					Err(err).
					Str("query_id", queryID).
					Msg("PostgresLogAnalyzeResponse ayrıştırma hatası")
				return nil, fmt.Errorf("sonuç ayrıştırma hatası: %v", err)
			}

			logger.Debug().
				Int("log_entries_count", len(analyzeResponse.LogEntries)).
				Str("query_id", queryID).
				Msg("Doğrudan PostgresLogAnalyzeResponse'a başarıyla ayrıştırıldı")
			return &analyzeResponse, nil
		}

		// Sonucun içeriğini logla
		json.Marshal(resultStruct.AsMap())

		// Struct'tan PostgresLogAnalyzeResponse oluştur
		logEntries := make([]*pb.PostgresLogEntry, 0)
		entriesValue, ok := resultStruct.Fields["log_entries"]
		if ok && entriesValue != nil && entriesValue.GetListValue() != nil {
			for _, entryValue := range entriesValue.GetListValue().Values {
				if entryValue.GetStructValue() != nil {
					entryStruct := entryValue.GetStructValue()

					// Log giriş değerlerini al
					timestamp := int64(entryStruct.Fields["timestamp"].GetNumberValue())
					logLevel := entryStruct.Fields["log_level"].GetStringValue()
					userName := entryStruct.Fields["user_name"].GetStringValue()
					database := entryStruct.Fields["database"].GetStringValue()
					processId := entryStruct.Fields["process_id"].GetStringValue()
					connectionFrom := entryStruct.Fields["connection_from"].GetStringValue()
					sessionId := entryStruct.Fields["session_id"].GetStringValue()
					sessionLineNum := entryStruct.Fields["session_line_num"].GetStringValue()
					commandTag := entryStruct.Fields["command_tag"].GetStringValue()
					sessionStartTime := entryStruct.Fields["session_start_time"].GetStringValue()
					virtualTransactionId := entryStruct.Fields["virtual_transaction_id"].GetStringValue()
					transactionId := entryStruct.Fields["transaction_id"].GetStringValue()
					errorSeverity := entryStruct.Fields["error_severity"].GetStringValue()
					sqlStateCode := entryStruct.Fields["sql_state_code"].GetStringValue()
					message := entryStruct.Fields["message"].GetStringValue()
					detail := entryStruct.Fields["detail"].GetStringValue()
					hint := entryStruct.Fields["hint"].GetStringValue()
					internalQuery := entryStruct.Fields["internal_query"].GetStringValue()
					durationMs := int64(entryStruct.Fields["duration_ms"].GetNumberValue())

					// PostgresLogEntry oluştur
					logEntry := &pb.PostgresLogEntry{
						Timestamp:            timestamp,
						LogLevel:             logLevel,
						UserName:             userName,
						Database:             database,
						ProcessId:            processId,
						ConnectionFrom:       connectionFrom,
						SessionId:            sessionId,
						SessionLineNum:       sessionLineNum,
						CommandTag:           commandTag,
						SessionStartTime:     sessionStartTime,
						VirtualTransactionId: virtualTransactionId,
						TransactionId:        transactionId,
						ErrorSeverity:        errorSeverity,
						SqlStateCode:         sqlStateCode,
						Message:              message,
						Detail:               detail,
						Hint:                 hint,
						InternalQuery:        internalQuery,
						DurationMs:           durationMs,
					}

					logEntries = append(logEntries, logEntry)
				}
			}
		}

		logger.Debug().
			Int("log_entries_count", len(logEntries)).
			Str("query_id", queryID).
			Str("agent_id", agentID).
			Msg("PostgreSQL log analizi struct'tan oluşturuldu")

		return &pb.PostgresLogAnalyzeResponse{
			LogEntries: logEntries,
		}, nil

	case <-ctx.Done():
		// Context iptal edildi veya zaman aşımına uğradı
		logger.Error().
			Err(ctx.Err()).
			Str("query_id", queryID).
			Str("agent_id", agentID).
			Msg("PostgreSQL log analizi context timeout")
		return nil, ctx.Err()
	}
}

// GetAlarms, veritabanından alarm kayıtlarını çeker
func (s *Server) GetAlarms(ctx context.Context, onlyUnacknowledged bool, limit, offset int, severityFilter, metricFilter string, dateFrom, dateTo *time.Time) ([]map[string]interface{}, int64, error) {
	// Veritabanı bağlantısını kontrol et
	if err := s.checkDatabaseConnection(); err != nil {
		return nil, 0, fmt.Errorf("veritabanı bağlantı hatası: %v", err)
	}

	// Varsayılan limit kontrolü
	if limit <= 0 || limit > 1000 {
		limit = 250 // Varsayılan 250 kayıt
	}
	if offset < 0 {
		offset = 0
	}

	// WHERE koşullarını ve parametreleri hazırla
	var whereConditions []string
	var queryParams []interface{}
	paramIndex := 1

	// Sadece acknowledge edilmemiş alarmları getir
	if onlyUnacknowledged {
		whereConditions = append(whereConditions, fmt.Sprintf("acknowledged = $%d", paramIndex))
		queryParams = append(queryParams, false)
		paramIndex++
	}

	// Severity filtresi
	if severityFilter != "" {
		whereConditions = append(whereConditions, fmt.Sprintf("severity = $%d", paramIndex))
		queryParams = append(queryParams, severityFilter)
		paramIndex++
	}

	// Metric name filtresi
	if metricFilter != "" {
		whereConditions = append(whereConditions, fmt.Sprintf("metric_name = $%d", paramIndex))
		queryParams = append(queryParams, metricFilter)
		paramIndex++
	}

	// Tarih aralığı filtresi
	if dateFrom != nil {
		whereConditions = append(whereConditions, fmt.Sprintf("created_at >= $%d", paramIndex))
		queryParams = append(queryParams, *dateFrom)
		paramIndex++
	}
	if dateTo != nil {
		whereConditions = append(whereConditions, fmt.Sprintf("created_at <= $%d", paramIndex))
		queryParams = append(queryParams, *dateTo)
		paramIndex++
	}

	// WHERE clause oluştur
	whereClause := ""
	if len(whereConditions) > 0 {
		whereClause = " WHERE " + strings.Join(whereConditions, " AND ")
	}

	// Önce toplam kayıt sayısını al (count query)
	countQuery := "SELECT COUNT(*) FROM alarms" + whereClause
	var totalCount int64
	err := s.db.QueryRowContext(ctx, countQuery, queryParams...).Scan(&totalCount)
	if err != nil {
		return nil, 0, fmt.Errorf("toplam alarm sayısı alınamadı: %v", err)
	}

	// Ana SQL sorgusu
	query := `
		SELECT 
			alarm_id,
			event_id,
			agent_id,
			status,
			metric_name,
			metric_value,
			message,
			severity,
			created_at,
			acknowledged,
			database
		FROM alarms` + whereClause + `
		ORDER BY created_at DESC
		LIMIT $` + fmt.Sprintf("%d", paramIndex) + ` OFFSET $` + fmt.Sprintf("%d", paramIndex+1)

	// LIMIT ve OFFSET parametrelerini ekle
	queryParams = append(queryParams, limit, offset)

	logger.Debug().
		Int("limit", limit).
		Int("offset", offset).
		Int64("total", totalCount).
		Str("severity_filter", severityFilter).
		Str("metric_filter", metricFilter).
		Msg("Alarm sorgusu")

	// Sorguyu çalıştır
	rows, err := s.db.QueryContext(ctx, query, queryParams...)
	if err != nil {
		return nil, 0, fmt.Errorf("alarm verileri çekilemedi: %v", err)
	}
	defer rows.Close()

	// Sonuçları topla
	alarms := make([]map[string]interface{}, 0)
	for rows.Next() {
		var (
			alarmID      string
			eventID      string
			agentID      string
			status       string
			metricName   string
			metricValue  string
			message      string
			severity     string
			createdAt    time.Time
			acknowledged bool
			database     sql.NullString // NULL değerleri kabul edebilmesi için NullString kullanıyoruz
		)

		// Satırı oku
		err := rows.Scan(
			&alarmID,
			&eventID,
			&agentID,
			&status,
			&metricName,
			&metricValue,
			&message,
			&severity,
			&createdAt,
			&acknowledged,
			&database,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("satır okuma hatası: %v", err)
		}

		// Database değerini kontrolle ekle
		var databaseValue string
		if database.Valid {
			databaseValue = database.String
		} else {
			databaseValue = "" // Eğer NULL ise boş string atanır
		}

		// Her alarmı map olarak oluştur
		alarm := map[string]interface{}{
			"alarm_id":     alarmID,
			"event_id":     eventID,
			"agent_id":     agentID,
			"status":       status,
			"metric_name":  metricName,
			"metric_value": metricValue,
			"message":      message,
			"severity":     severity,
			"created_at":   createdAt.Format(time.RFC3339),
			"acknowledged": acknowledged,
			"database":     databaseValue,
		}

		alarms = append(alarms, alarm)
	}

	// Satır okuma hatası kontrolü
	if err = rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("satır okuma hatası: %v", err)
	}

	return alarms, totalCount, nil
}

// GetAlarmsStatus, alarm verilerini döndürür
func (s *Server) GetAlarmsStatus(ctx context.Context, _ *structpb.Struct) (*structpb.Value, error) {
	// Alarmları getir - Son 100 alarmı al
	alarms, _, err := s.GetAlarms(ctx, false, 100, 0, "", "", nil, nil)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Alarm verileri alınamadı: %v", err)
	}

	// JSON verisini structpb.Value'ya dönüştür
	value, err := structpb.NewValue(alarms)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Veri dönüştürme hatası: %v", err)
	}

	return value, nil
}

// AcknowledgeAlarm, belirtilen event_id'ye sahip alarmı acknowledge eder
func (s *Server) AcknowledgeAlarm(ctx context.Context, eventID string) error {
	// Veritabanı bağlantısını kontrol et
	if err := s.checkDatabaseConnection(); err != nil {
		return fmt.Errorf("veritabanı bağlantı hatası: %v", err)
	}

	// SQL sorgusu
	query := `
		UPDATE alarms 
		SET acknowledged = true 
		WHERE event_id = $1
	`

	result, err := s.db.ExecContext(ctx, query, eventID)
	if err != nil {
		return fmt.Errorf("alarm güncellenemedi: %v", err)
	}

	// Etkilenen satır sayısını kontrol et
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("etkilenen satır sayısı alınamadı: %v", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("belirtilen event_id ile alarm bulunamadı: %s", eventID)
	}

	return nil
}

// ReadPostgresConfig, belirtilen PostgreSQL config dosyasını okur
func (s *Server) ReadPostgresConfig(ctx context.Context, req *pb.PostgresConfigRequest) (*pb.PostgresConfigResponse, error) {
	// Agent ID'yi kontrol et
	agentID := req.AgentId
	if agentID == "" {
		return nil, fmt.Errorf("agent_id gerekli")
	}

	// Config dosya yolunu kontrol et
	configPath := req.ConfigPath
	if configPath == "" {
		return nil, fmt.Errorf("config_path gerekli")
	}

	// Config dosyasını okuma isteğini gönder
	return s.sendPostgresConfigQuery(ctx, agentID, configPath)
}

// sendPostgresConfigQuery, agent'a PostgreSQL config dosyasını okuması için sorgu gönderir
func (s *Server) sendPostgresConfigQuery(ctx context.Context, agentID, configPath string) (*pb.PostgresConfigResponse, error) {
	// Agent'ın bağlı olup olmadığını kontrol et
	s.mu.RLock()
	agent, agentExists := s.agents[agentID]
	s.mu.RUnlock()

	if !agentExists || agent == nil {
		return nil, fmt.Errorf("agent bulunamadı veya bağlantı kapalı: %s", agentID)
	}

	// configPath boş mu kontrol et
	if configPath == "" {
		return nil, fmt.Errorf("config dosya yolu boş olamaz")
	}

	// PostgreSQL config okuması için bir komut oluştur
	command := fmt.Sprintf("read_postgres_config|%s", configPath)
	queryID := fmt.Sprintf("postgres_config_%d", time.Now().UnixNano())

	logger.Debug().
		Str("command", command).
		Str("agent_id", agentID).
		Str("query_id", queryID).
		Str("config_path", configPath).
		Msg("PostgreSQL config okuması için komut")

	// Sonuç kanalı oluştur
	resultChan := make(chan *pb.QueryResult, 1)
	s.queryMu.Lock()
	s.queryResult[queryID] = &QueryResponse{
		ResultChan: resultChan,
	}
	s.queryMu.Unlock()

	// Context'in iptal durumunda kaynakları temizle
	defer func() {
		s.queryMu.Lock()
		delete(s.queryResult, queryID)
		s.queryMu.Unlock()
		close(resultChan)
	}()

	// Sorguyu gönder
	err := agent.Stream.Send(&pb.ServerMessage{
		Payload: &pb.ServerMessage_Query{
			Query: &pb.Query{
				QueryId: queryID,
				Command: command,
			},
		},
	})
	if err != nil {
		logger.Error().
			Err(err).
			Str("agent_id", agentID).
			Str("query_id", queryID).
			Str("config_path", configPath).
			Msg("PostgreSQL config sorgusu gönderilemedi")
		return nil, fmt.Errorf("sorgu gönderilemedi: %v", err)
	}

	logger.Info().
		Str("agent_id", agentID).
		Str("config_path", configPath).
		Str("query_id", queryID).
		Msg("PostgreSQL config için sorgu gönderildi")

	// Cevabı bekle
	select {
	case result := <-resultChan:
		// Sonuç geldi
		if result == nil {
			logger.Error().
				Str("query_id", queryID).
				Str("agent_id", agentID).
				Str("config_path", configPath).
				Msg("PostgreSQL config query null sonuç")
			return nil, fmt.Errorf("null sorgu sonucu alındı")
		}

		logger.Debug().
			Str("query_id", queryID).
			Str("type_url", result.Result.TypeUrl).
			Str("agent_id", agentID).
			Msg("PostgreSQL config query yanıt alındı")

		// Önce struct olarak ayrıştırmayı dene (Agent'ın gönderdiği tipte)
		var resultStruct structpb.Struct
		if err := result.Result.UnmarshalTo(&resultStruct); err != nil {
			logger.Debug().
				Err(err).
				Str("query_id", queryID).
				Str("agent_id", agentID).
				Msg("PostgreSQL config struct ayrıştırma hatası")

			// Struct ayrıştırma başarısız olursa, PostgresConfigResponse olarak dene
			var configResponse pb.PostgresConfigResponse
			if err := result.Result.UnmarshalTo(&configResponse); err != nil {
				logger.Error().
					Err(err).
					Str("query_id", queryID).
					Msg("PostgresConfigResponse ayrıştırma hatası")
				return nil, fmt.Errorf("sonuç ayrıştırma hatası: %v", err)
			}

			logger.Debug().
				Int("config_entries_count", len(configResponse.Configurations)).
				Str("query_id", queryID).
				Msg("Doğrudan PostgresConfigResponse'a başarıyla ayrıştırıldı")
			return &configResponse, nil
		}

		// Struct'tan PostgresConfigResponse oluştur
		configEntries := make([]*pb.PostgresConfigEntry, 0)
		entriesValue, ok := resultStruct.Fields["configurations"]
		if ok && entriesValue != nil && entriesValue.GetListValue() != nil {
			for _, entryValue := range entriesValue.GetListValue().Values {
				if entryValue.GetStructValue() != nil {
					entryStruct := entryValue.GetStructValue()

					// Config giriş değerlerini al
					parameter := entryStruct.Fields["parameter"].GetStringValue()
					value := entryStruct.Fields["value"].GetStringValue()
					description := entryStruct.Fields["description"].GetStringValue()
					isDefault := entryStruct.Fields["is_default"].GetBoolValue()
					category := entryStruct.Fields["category"].GetStringValue()

					// PostgresConfigEntry oluştur
					configEntry := &pb.PostgresConfigEntry{
						Parameter:   parameter,
						Value:       value,
						Description: description,
						IsDefault:   isDefault,
						Category:    category,
					}

					configEntries = append(configEntries, configEntry)
				}
			}
		}

		logger.Debug().
			Int("config_entries_count", len(configEntries)).
			Str("query_id", queryID).
			Str("agent_id", agentID).
			Msg("PostgreSQL config struct'tan oluşturuldu")

		// Config dosya yolunu al
		configPathValue := ""
		if pathValue, exists := resultStruct.Fields["config_path"]; exists {
			configPathValue = pathValue.GetStringValue()
		}

		return &pb.PostgresConfigResponse{
			Status:         "success",
			ConfigPath:     configPathValue,
			Configurations: configEntries,
		}, nil

	case <-ctx.Done():
		// Context iptal edildi veya zaman aşımına uğradı
		logger.Error().
			Err(ctx.Err()).
			Str("query_id", queryID).
			Str("agent_id", agentID).
			Str("config_path", configPath).
			Msg("PostgreSQL config query context timeout")
		return nil, ctx.Err()
	}
}

// ReportVersion, agent'ın versiyon bilgilerini işler
func (s *Server) ReportVersion(ctx context.Context, req *pb.ReportVersionRequest) (*pb.ReportVersionResponse, error) {
	logger.Info().
		Str("agent_id", req.AgentId).
		Msg("ReportVersion metodu çağrıldı")

	// Veritabanı bağlantısını kontrol et
	if err := s.checkDatabaseConnection(); err != nil {
		logger.Error().
			Err(err).
			Str("agent_id", req.AgentId).
			Msg("Veritabanı bağlantı hatası")
		return &pb.ReportVersionResponse{
			Status: "error",
		}, fmt.Errorf("veritabanı bağlantı hatası: %v", err)
	}

	// Versiyon bilgilerini logla
	versionInfo := req.VersionInfo
	logger.Info().
		Str("agent_id", req.AgentId).
		Str("version", versionInfo.Version).
		Str("platform", versionInfo.Platform).
		Str("architecture", versionInfo.Architecture).
		Str("hostname", versionInfo.Hostname).
		Str("os_version", versionInfo.OsVersion).
		Str("go_version", versionInfo.GoVersion).
		Msg("Agent versiyon bilgileri alındı")

	// Versiyon bilgilerini veritabanına kaydet
	query := `
		INSERT INTO agent_versions (
			agent_id,
			version,
			platform,
			architecture,
			hostname,
			os_version,
			go_version,
			reported_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, CURRENT_TIMESTAMP)
		ON CONFLICT (agent_id) DO UPDATE SET
			version = EXCLUDED.version,
			platform = EXCLUDED.platform,
			architecture = EXCLUDED.architecture,
			hostname = EXCLUDED.hostname,
			os_version = EXCLUDED.os_version,
			go_version = EXCLUDED.go_version,
			reported_at = CURRENT_TIMESTAMP
	`

	_, err := s.db.ExecContext(ctx, query,
		req.AgentId,
		versionInfo.Version,
		versionInfo.Platform,
		versionInfo.Architecture,
		versionInfo.Hostname,
		versionInfo.OsVersion,
		versionInfo.GoVersion,
	)

	if err != nil {
		logger.Error().
			Err(err).
			Str("agent_id", req.AgentId).
			Msg("Versiyon bilgileri kaydedilemedi")
		return &pb.ReportVersionResponse{
			Status: "error",
		}, fmt.Errorf("versiyon bilgileri kaydedilemedi: %v", err)
	}

	logger.Info().
		Str("agent_id", req.AgentId).
		Str("version", versionInfo.Version).
		Msg("Agent versiyon bilgileri başarıyla kaydedildi")
	return &pb.ReportVersionResponse{
		Status: "success",
	}, nil
}

// GetAgentVersions, veritabanından agent versiyon bilgilerini çeker
func (s *Server) GetAgentVersions(ctx context.Context) ([]map[string]interface{}, error) {
	// Veritabanı bağlantısını kontrol et
	if err := s.checkDatabaseConnection(); err != nil {
		return nil, fmt.Errorf("veritabanı bağlantı hatası: %v", err)
	}

	// SQL sorgusu
	query := `
		SELECT 
			agent_id,
			version,
			platform,
			architecture,
			hostname,
			os_version,
			go_version,
			reported_at,
			created_at,
			updated_at
		FROM agent_versions
		ORDER BY reported_at DESC
	`

	// Sorguyu çalıştır
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("versiyon bilgileri çekilemedi: %v", err)
	}
	defer rows.Close()

	// Sonuçları topla
	versions := make([]map[string]interface{}, 0)
	for rows.Next() {
		var (
			agentID      string
			version      string
			platform     string
			architecture string
			hostname     string
			osVersion    string
			goVersion    string
			reportedAt   time.Time
			createdAt    time.Time
			updatedAt    time.Time
		)

		// Satırı oku
		err := rows.Scan(
			&agentID,
			&version,
			&platform,
			&architecture,
			&hostname,
			&osVersion,
			&goVersion,
			&reportedAt,
			&createdAt,
			&updatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("satır okuma hatası: %v", err)
		}

		// Her versiyonu map olarak oluştur
		versionInfo := map[string]interface{}{
			"agent_id":     agentID,
			"version":      version,
			"platform":     platform,
			"architecture": architecture,
			"hostname":     hostname,
			"os_version":   osVersion,
			"go_version":   goVersion,
			"reported_at":  reportedAt.Format(time.RFC3339),
			"created_at":   createdAt.Format(time.RFC3339),
			"updated_at":   updatedAt.Format(time.RFC3339),
		}

		versions = append(versions, versionInfo)
	}

	// Satır okuma hatası kontrolü
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("satır okuma hatası: %v", err)
	}

	return versions, nil
}

// PromoteMongoToPrimary, MongoDB node'unu primary'ye yükseltir
func (s *Server) PromoteMongoToPrimary(ctx context.Context, req *pb.MongoPromotePrimaryRequest) (*pb.MongoPromotePrimaryResponse, error) {
	// Job oluştur
	job := &pb.Job{
		JobId:     req.JobId,
		Type:      pb.JobType_JOB_TYPE_MONGO_PROMOTE_PRIMARY,
		Status:    pb.JobStatus_JOB_STATUS_PENDING,
		AgentId:   req.AgentId,
		CreatedAt: timestamppb.Now(),
		UpdatedAt: timestamppb.Now(),
		Parameters: map[string]string{
			"node_hostname": req.NodeHostname,
			"port":          fmt.Sprintf("%d", req.Port),
			"replica_set":   req.ReplicaSet,
			"node_status":   req.NodeStatus, // Node status'ını ekle
		},
	}

	// Job'ı kaydet
	s.jobMu.Lock()
	s.jobs[job.JobId] = job
	s.jobMu.Unlock()

	// İlgili agent'ı bul
	s.mu.RLock()
	agent, exists := s.agents[req.AgentId]
	s.mu.RUnlock()

	if !exists {
		job.Status = pb.JobStatus_JOB_STATUS_FAILED
		job.ErrorMessage = "Agent bulunamadı"
		job.UpdatedAt = timestamppb.Now()
		return &pb.MongoPromotePrimaryResponse{
			JobId:        job.JobId,
			Status:       job.Status,
			ErrorMessage: job.ErrorMessage,
		}, fmt.Errorf("agent bulunamadı: %s", req.AgentId)
	}

	// Job'ı veritabanına kaydet
	if err := s.saveJobToDatabase(ctx, job); err != nil {
		job.Status = pb.JobStatus_JOB_STATUS_FAILED
		job.ErrorMessage = fmt.Sprintf("Job veritabanına kaydedilemedi: %v", err)
		job.UpdatedAt = timestamppb.Now()
		return &pb.MongoPromotePrimaryResponse{
			JobId:        job.JobId,
			Status:       job.Status,
			ErrorMessage: job.ErrorMessage,
		}, err
	}

	// Job'ı çalıştır
	go func() {
		job.Status = pb.JobStatus_JOB_STATUS_RUNNING

		// MongoDB komutu oluştur - node_status'a göre farklı komut gönder
		var command string
		if req.NodeStatus == "PRIMARY" {
			// Eğer node primary ise, primary'i step down yapalım
			command = "rs.stepDown()"
		} else {
			// Secondary node durumunda bir şey yapmaya gerek yok (freeze endpoint'i yoluyla hallolacak)
			command = fmt.Sprintf("db.hello() // Node %s secondary durumunda", req.NodeHostname)
		}

		// Agent'a promote isteği gönder
		err := agent.Stream.Send(&pb.ServerMessage{
			Payload: &pb.ServerMessage_Query{
				Query: &pb.Query{
					QueryId: job.JobId,
					Command: command,
				},
			},
		})

		if err != nil {
			job.Status = pb.JobStatus_JOB_STATUS_FAILED
			job.ErrorMessage = fmt.Sprintf("Agent'a istek gönderilemedi: %v", err)
		} else {
			job.Status = pb.JobStatus_JOB_STATUS_COMPLETED
			job.Result = "Primary promotion request sent successfully"
		}

		job.UpdatedAt = timestamppb.Now()
		s.updateJobInDatabase(context.Background(), job)
	}()

	return &pb.MongoPromotePrimaryResponse{
		JobId:  job.JobId,
		Status: pb.JobStatus_JOB_STATUS_PENDING,
	}, nil
}

// PromotePostgresToMaster, PostgreSQL node'unu master'a yükseltir
//
// Yeni JSON formatı:
//
//	{
//	  "agent_id": "agent_slave_hostname",
//	  "node_hostname": "slave_hostname",
//	  "data_directory": "/var/lib/postgresql/data",
//	  "current_master_host": "master_hostname",
//	  "current_master_ip": "master_ip_address",
//	  "slaves": [
//	    {"hostname": "other_slave1_hostname", "ip": "other_slave1_ip"},
//	    {"hostname": "other_slave2_hostname", "ip": "other_slave2_ip"}
//	  ]
//	}
//
// ✅ Protobuf güncellendi ve yeni alanlar eklendi:
// - CurrentMasterIp string
// - Slaves []*SlaveNode (SlaveNode: Hostname, Ip alanları olan struct)
//
// NOT: Replication user/password bilgileri artık güvenlik nedeniyle agent config'inden alınacak
func (s *Server) PromotePostgresToMaster(ctx context.Context, req *pb.PostgresPromoteMasterRequest) (*pb.PostgresPromoteMasterResponse, error) {
	logger.Info().
		Str("agent_id", req.AgentId).
		Str("node_hostname", req.NodeHostname).
		Str("data_directory", req.DataDirectory).
		Str("current_master_host", req.CurrentMasterHost).
		Str("current_master_ip", req.CurrentMasterIp).
		Int("slave_count", len(req.Slaves)).
		Str("job_id", req.JobId).
		Msg("PromotePostgresToMaster çağrıldı")

	// DEBUG: Request içeriğini detaylı logla
	logger.Debug().
		Str("agent_id", req.AgentId).
		Str("node_hostname", req.NodeHostname).
		Str("data_directory", req.DataDirectory).
		Str("current_master_host", req.CurrentMasterHost).
		Str("current_master_ip", req.CurrentMasterIp).
		Int("slaves_count", len(req.Slaves)).
		Interface("slaves_raw", req.Slaves).
		Str("job_id", req.JobId).
		Msg("DEBUG: PromotePostgresToMaster request detayları")

	// EXTRA DEBUG: Her slave'i ayrı ayrı logla
	for i, slave := range req.Slaves {
		logger.Debug().
			Int("slave_index", i).
			Str("slave_hostname", slave.Hostname).
			Str("slave_ip", slave.Ip).
			Msg("DEBUG: Individual slave node")
	}

	// EXTRA DEBUG: Request'in nil olup olmadığını kontrol et
	if req.CurrentMasterIp == "" {
		logger.Warn().Msg("DEBUG: current_master_ip is EMPTY!")
	}
	if len(req.Slaves) == 0 {
		logger.Warn().Msg("DEBUG: slaves array is EMPTY!")
	}

	// Slave bilgilerini logla
	if len(req.Slaves) > 0 {
		for i, slave := range req.Slaves {
			logger.Debug().
				Int("slave_index", i).
				Str("slave_hostname", slave.Hostname).
				Str("slave_ip", slave.Ip).
				Msg("Other slave node")
		}
	}

	// Process log takibi için metadata oluştur
	metadata := map[string]string{
		"node_hostname":       req.NodeHostname,
		"data_directory":      req.DataDirectory,
		"job_id":              req.JobId,
		"current_master_host": req.CurrentMasterHost,
		"current_master_ip":   req.CurrentMasterIp,
		"slave_count":         fmt.Sprintf("%d", len(req.Slaves)),
	}

	// Slave bilgilerini metadata'ya ekle
	for i, slave := range req.Slaves {
		metadata[fmt.Sprintf("slave_%d_hostname", i)] = slave.Hostname
		metadata[fmt.Sprintf("slave_%d_ip", i)] = slave.Ip
	}

	// Job oluştur
	job := &pb.Job{
		JobId:     req.JobId,
		Type:      pb.JobType_JOB_TYPE_POSTGRES_PROMOTE_MASTER,
		Status:    pb.JobStatus_JOB_STATUS_PENDING,
		AgentId:   req.AgentId,
		CreatedAt: timestamppb.Now(),
		UpdatedAt: timestamppb.Now(),
		Parameters: map[string]string{
			"node_hostname":       req.NodeHostname,
			"data_directory":      req.DataDirectory,
			"process_id":          req.JobId,                          // Process ID olarak job ID'yi kullan
			"current_master_host": req.CurrentMasterHost,              // Eski master bilgisi
			"current_master_ip":   req.CurrentMasterIp,                // Eski master IP bilgisi
			"slave_count":         fmt.Sprintf("%d", len(req.Slaves)), // Diğer slave sayısı
			// Replication user/password artık agent config'inden alınacak (güvenlik)
		},
	}

	// Slave bilgilerini job parameters'a ekle
	for i, slave := range req.Slaves {
		job.Parameters[fmt.Sprintf("slave_%d_hostname", i)] = slave.Hostname
		job.Parameters[fmt.Sprintf("slave_%d_ip", i)] = slave.Ip
	}

	// Job'ı kaydet
	s.jobMu.Lock()
	s.jobs[job.JobId] = job
	s.jobMu.Unlock()

	// İlgili agent'ı bul
	s.mu.RLock()
	agent, exists := s.agents[req.AgentId]
	s.mu.RUnlock()

	if !exists {
		job.Status = pb.JobStatus_JOB_STATUS_FAILED
		job.ErrorMessage = "Agent bulunamadı"
		job.UpdatedAt = timestamppb.Now()

		// Process log oluştur - hata durumu
		processLog := &pb.ProcessLogUpdate{
			AgentId:      req.AgentId,
			ProcessId:    req.JobId,
			ProcessType:  "postgresql_promotion",
			Status:       "failed",
			LogMessages:  []string{"Agent bulunamadı, işlem başlatılamadı"},
			ElapsedTimeS: 0,
			UpdatedAt:    time.Now().Format(time.RFC3339),
			Metadata:     metadata,
		}
		s.saveProcessLogs(ctx, processLog)

		return &pb.PostgresPromoteMasterResponse{
			JobId:        job.JobId,
			Status:       job.Status,
			ErrorMessage: job.ErrorMessage,
		}, fmt.Errorf("agent bulunamadı: %s", req.AgentId)
	}

	// Job'ı veritabanına kaydet
	if err := s.saveJobToDatabase(ctx, job); err != nil {
		job.Status = pb.JobStatus_JOB_STATUS_FAILED
		job.ErrorMessage = fmt.Sprintf("Job veritabanına kaydedilemedi: %v", err)
		job.UpdatedAt = timestamppb.Now()

		// Process log oluştur - veritabanı hatası
		processLog := &pb.ProcessLogUpdate{
			AgentId:      req.AgentId,
			ProcessId:    req.JobId,
			ProcessType:  "postgresql_promotion",
			Status:       "failed",
			LogMessages:  []string{fmt.Sprintf("Job veritabanına kaydedilemedi: %v", err)},
			ElapsedTimeS: 0,
			UpdatedAt:    time.Now().Format(time.RFC3339),
			Metadata:     metadata,
		}
		s.saveProcessLogs(ctx, processLog)

		return &pb.PostgresPromoteMasterResponse{
			JobId:        job.JobId,
			Status:       job.Status,
			ErrorMessage: job.ErrorMessage,
		}, err
	}

	// Process log başlangıç kaydı oluştur
	startLog := &pb.ProcessLogUpdate{
		AgentId:      req.AgentId,
		ProcessId:    req.JobId,
		ProcessType:  "postgresql_promotion",
		Status:       "running",
		LogMessages:  []string{fmt.Sprintf("PostgreSQL promotion işlemi başlatılıyor - Node: %s", req.NodeHostname)},
		ElapsedTimeS: 0,
		UpdatedAt:    time.Now().Format(time.RFC3339),
		Metadata:     metadata,
	}
	s.saveProcessLogs(ctx, startLog)

	// Job'ı çalıştır
	go func() {
		job.Status = pb.JobStatus_JOB_STATUS_RUNNING
		job.UpdatedAt = timestamppb.Now()

		// 🔧 FIX: Database update için timeout context kullan
		dbCtx, dbCancel := context.WithTimeout(context.Background(), 5*time.Second)
		s.updateJobInDatabase(dbCtx, job)
		dbCancel()

		// Yeni komut formatı: "postgres_promote|data_dir|process_id|old_master_host|old_master_ip|slave_count|slaves_info"
		// Agent tarafındaki ProcessLogger'ı etkinleştirmek için process_id gönderiyoruz
		// Eski master bilgisini ve diğer slave bilgilerini de gönderiyoruz (koordinasyon için)
		// Agent kendi config'inden replication bilgilerini alacak

		// Slave bilgilerini string formatına çevir
		slave_info := ""
		if len(req.Slaves) > 0 {
			var slaveInfos []string
			for _, slave := range req.Slaves {
				slaveInfos = append(slaveInfos, fmt.Sprintf("%s:%s", slave.Hostname, slave.Ip))
			}
			slave_info = strings.Join(slaveInfos, ",")
		}

		// Tam komut formatı: postgres_promote|data_dir|process_id|new_master_host|old_master_host|old_master_ip|slave_count|slaves_info
		command := fmt.Sprintf("postgres_promote|%s|%s|%s|%s|%s|%d|%s",
			req.DataDirectory, req.JobId, req.NodeHostname, req.CurrentMasterHost, req.CurrentMasterIp, len(req.Slaves), slave_info)

		// Slave bilgilerini ve komut detaylarını logla
		logger.Info().
			Str("job_id", req.JobId).
			Str("agent_id", req.AgentId).
			Str("new_master_host", req.NodeHostname).
			Str("old_master_host", req.CurrentMasterHost).
			Str("old_master_ip", req.CurrentMasterIp).
			Str("slave_info", slave_info).
			Int("slave_count", len(req.Slaves)).
			Str("command", command).
			Msg("PostgreSQL promotion komutu agent'a gönderiliyor")

		// 🔧 FIX: Timeout ile Stream.Send() çağrısı
		sendCtx, sendCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer sendCancel()

		// Channel kullanarak timeout kontrollü gönderme
		sendDone := make(chan error, 1)
		go func() {
			err := agent.Stream.Send(&pb.ServerMessage{
				Payload: &pb.ServerMessage_Query{
					Query: &pb.Query{
						QueryId: job.JobId,
						Command: command,
					},
				},
			})
			sendDone <- err
		}()

		// Timeout veya başarı durumunu bekle
		select {
		case err := <-sendDone:
			if err != nil {
				job.Status = pb.JobStatus_JOB_STATUS_FAILED
				job.ErrorMessage = fmt.Sprintf("Agent'a istek gönderilemedi: %v", err)
				job.UpdatedAt = timestamppb.Now()

				// Database update için timeout context
				dbCtx2, dbCancel2 := context.WithTimeout(context.Background(), 5*time.Second)
				s.updateJobInDatabase(dbCtx2, job)
				dbCancel2()

				// Hata durumu process log'u
				errorLog := &pb.ProcessLogUpdate{
					AgentId:      req.AgentId,
					ProcessId:    req.JobId,
					ProcessType:  "postgresql_promotion",
					Status:       "failed",
					LogMessages:  []string{fmt.Sprintf("Agent'a istek gönderilemedi: %v", err)},
					ElapsedTimeS: 0,
					UpdatedAt:    time.Now().Format(time.RFC3339),
					Metadata:     metadata,
				}
				// Process logs için de timeout context
				logCtx, logCancel := context.WithTimeout(context.Background(), 5*time.Second)
				s.saveProcessLogs(logCtx, errorLog)
				logCancel()
			} else {
				// Başarılı başlatma durumu
				logger.Info().
					Str("job_id", job.JobId).
					Str("agent_id", req.AgentId).
					Str("node_hostname", req.NodeHostname).
					Msg("PostgreSQL promotion işlemi agent'a iletildi")

				// Job'ı IN_PROGRESS olarak işaretle
				// Agent ProcessLogger ile ilerleyişi bildirecek, burada bir şey yapmamıza gerek yok

				// Tamamlandı olarak işaretleme işlemini artık agent tarafından gelen
				// son log mesajı (completed statüsünde) ile yapacağız.
			}
		case <-sendCtx.Done():
			logger.Error().
				Str("job_id", job.JobId).
				Str("agent_id", req.AgentId).
				Dur("timeout", 10*time.Second).
				Msg("Timeout: PostgreSQL promotion agent'a gönderilemedi")
			job.Status = pb.JobStatus_JOB_STATUS_FAILED
			job.ErrorMessage = "Timeout: Agent'a istek gönderilemedi (10s timeout)"
			job.UpdatedAt = timestamppb.Now()

			// Database update için timeout context
			dbCtx3, dbCancel3 := context.WithTimeout(context.Background(), 5*time.Second)
			s.updateJobInDatabase(dbCtx3, job)
			dbCancel3()

			// Timeout durumu process log'u
			timeoutLog := &pb.ProcessLogUpdate{
				AgentId:      req.AgentId,
				ProcessId:    req.JobId,
				ProcessType:  "postgresql_promotion",
				Status:       "failed",
				LogMessages:  []string{"Timeout: Agent'a istek gönderilemedi (10s timeout)"},
				ElapsedTimeS: 0,
				UpdatedAt:    time.Now().Format(time.RFC3339),
				Metadata:     metadata,
			}
			// Process logs için de timeout context
			logCtx2, logCancel2 := context.WithTimeout(context.Background(), 5*time.Second)
			s.saveProcessLogs(logCtx2, timeoutLog)
			logCancel2()
		}
	}()

	return &pb.PostgresPromoteMasterResponse{
		JobId:  job.JobId,
		Status: pb.JobStatus_JOB_STATUS_PENDING,
	}, nil
}

// ConvertPostgresToSlave PostgreSQL master'ı slave'e dönüştürür
func (s *Server) ConvertPostgresToSlave(ctx context.Context, req *pb.ConvertPostgresToSlaveRequest) (*pb.ConvertPostgresToSlaveResponse, error) {
	logger.Info().
		Str("agent_id", req.AgentId).
		Str("node_hostname", req.NodeHostname).
		Str("new_master_host", req.NewMasterHost).
		Str("new_master_ip", req.NewMasterIp).
		Int32("new_master_port", req.NewMasterPort).
		Str("coordination_job_id", req.CoordinationJobId).
		Str("old_master_host", req.OldMasterHost).
		Str("job_id", req.JobId).
		Msg("ConvertPostgresToSlave çağrıldı")

	// Job oluştur
	job := &pb.Job{
		JobId:     req.JobId,
		Type:      pb.JobType_JOB_TYPE_POSTGRES_CONVERT_TO_SLAVE,
		Status:    pb.JobStatus_JOB_STATUS_PENDING,
		AgentId:   req.AgentId,
		CreatedAt: timestamppb.Now(),
		UpdatedAt: timestamppb.Now(),
		Parameters: map[string]string{
			"node_hostname":       req.NodeHostname,
			"new_master_host":     req.NewMasterHost,
			"new_master_ip":       req.NewMasterIp,
			"new_master_port":     fmt.Sprintf("%d", req.NewMasterPort),
			"data_directory":      req.DataDirectory,
			"coordination_job_id": req.CoordinationJobId,
			"old_master_host":     req.OldMasterHost,
			// replication_user KALDIRILDI - artık agent config'den okunacak
		},
	}

	// Agent'ı bul ve komut gönder
	s.mu.RLock()
	agent, exists := s.agents[req.AgentId]
	s.mu.RUnlock()

	if !exists {
		return &pb.ConvertPostgresToSlaveResponse{
			JobId:        req.JobId,
			Status:       pb.JobStatus_JOB_STATUS_FAILED,
			ErrorMessage: "Agent bulunamadı",
		}, fmt.Errorf("agent bulunamadı: %s", req.AgentId)
	}

	// Komutu oluştur ve gönder (yeni format)
	// NOT: Replication bilgileri artık agent config'inden alınacak (güvenlik)
	command := fmt.Sprintf("convert_postgres_to_slave|%s|%s|%d|%s|%s|%s",
		req.NewMasterHost,     // new_master_host
		req.NewMasterIp,       // new_master_ip (YENİ)
		req.NewMasterPort,     // new_master_port (INT)
		req.DataDirectory,     // data_dir
		req.CoordinationJobId, // coordination_job_id (opsiyonel)
		req.OldMasterHost)     // old_master_host (opsiyonel)

	// 🔧 FIX: Timeout ile Stream.Send() çağrısı
	sendCtx, sendCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer sendCancel()

	// Channel kullanarak timeout kontrollü gönderme
	sendDone := make(chan error, 1)
	go func() {
		err := agent.Stream.Send(&pb.ServerMessage{
			Payload: &pb.ServerMessage_Query{
				Query: &pb.Query{
					QueryId: req.JobId,
					Command: command,
				},
			},
		})
		sendDone <- err
	}()

	// Timeout veya başarı durumunu bekle
	select {
	case err := <-sendDone:
		if err != nil {
			job.Status = pb.JobStatus_JOB_STATUS_FAILED
			job.ErrorMessage = fmt.Sprintf("Agent'a komut gönderilemedi: %v", err)
		} else {
			job.Status = pb.JobStatus_JOB_STATUS_RUNNING
		}
	case <-sendCtx.Done():
		logger.Error().
			Str("job_id", req.JobId).
			Str("agent_id", req.AgentId).
			Dur("timeout", 10*time.Second).
			Msg("Timeout: ConvertPostgresToSlave agent'a gönderilemedi")
		job.Status = pb.JobStatus_JOB_STATUS_FAILED
		job.ErrorMessage = "Timeout: Agent'a komut gönderilemedi (10s timeout)"
	}

	job.UpdatedAt = timestamppb.Now()
	s.jobMu.Lock()
	s.jobs[job.JobId] = job
	s.jobMu.Unlock()

	return &pb.ConvertPostgresToSlaveResponse{
		JobId:        job.JobId,
		Status:       job.Status,
		ErrorMessage: job.ErrorMessage,
	}, nil
}

// GetJob, belirli bir job'ın detaylarını getirir
func (s *Server) GetJob(ctx context.Context, req *pb.GetJobRequest) (*pb.GetJobResponse, error) {
	s.jobMu.RLock()
	job, exists := s.jobs[req.JobId]
	s.jobMu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("job bulunamadı: %s", req.JobId)
	}

	return &pb.GetJobResponse{
		Job: job,
	}, nil
}

// ListJobs, job listesini veritabanından doğrudan sorgular ve getirir
func (s *Server) ListJobs(ctx context.Context, req *pb.ListJobsRequest) (*pb.ListJobsResponse, error) {
	logger.Debug().
		Str("agent_id", req.AgentId).
		Interface("status", req.Status).
		Interface("type", req.Type).
		Int32("limit", req.Limit).
		Int32("offset", req.Offset).
		Msg("ListJobs çağrıldı")

	// Veritabanı bağlantısını kontrol et
	if err := s.checkDatabaseConnection(); err != nil {
		return nil, fmt.Errorf("veritabanı bağlantı hatası: %v", err)
	}

	// Sorgu için parametreleri hazırla
	var queryParams []interface{}
	var conditions []string

	// Temel sorgu oluştur
	baseQuery := `
		SELECT 
			job_id, type, status, agent_id, 
			created_at, updated_at, error_message, 
			parameters, result
		FROM jobs
	`

	// WHERE koşulları oluştur
	paramCount := 1 // PostgreSQL parametreleri $1, $2, ... şeklinde başlar

	// Agent ID filtresi ekle
	if req.AgentId != "" {
		conditions = append(conditions, fmt.Sprintf("agent_id = $%d", paramCount))
		queryParams = append(queryParams, req.AgentId)
		paramCount++
	}

	// Status filtresi ekle
	if req.Status != pb.JobStatus_JOB_STATUS_UNKNOWN {
		conditions = append(conditions, fmt.Sprintf("status = $%d", paramCount))
		queryParams = append(queryParams, req.Status.String())
		paramCount++
	}

	// Type filtresi ekle
	if req.Type != pb.JobType_JOB_TYPE_UNKNOWN {
		conditions = append(conditions, fmt.Sprintf("type = $%d", paramCount))
		queryParams = append(queryParams, req.Type.String())
		paramCount++
	}

	// WHERE koşullarını SQL sorgusuna ekle
	query := baseQuery
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	// Toplam kayıt sayısını almak için COUNT sorgusu
	countQuery := "SELECT COUNT(*) FROM jobs"
	if len(conditions) > 0 {
		countQuery += " WHERE " + strings.Join(conditions, " AND ")
	}

	// Önce toplam kayıt sayısını al
	var total int32
	err := s.db.QueryRowContext(ctx, countQuery, queryParams...).Scan(&total)
	if err != nil {
		logger.Error().
			Err(err).
			Str("agent_id", req.AgentId).
			Msg("Job sayısı alınamadı")
		return nil, fmt.Errorf("job sayısı alınamadı: %v", err)
	}
	logger.Debug().
		Int64("total_jobs", int64(total)).
		Msg("Filtrelere göre toplam job sayısı")

	// Sıralama ve limit ekle
	query += " ORDER BY created_at DESC"

	// Varsayılan değerler
	limit := req.Limit
	if limit <= 0 {
		limit = 10 // Varsayılan limit
	}

	offset := req.Offset
	if offset < 0 {
		offset = 0
	}

	query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", paramCount, paramCount+1)
	queryParams = append(queryParams, limit, offset)

	logger.Debug().
		Str("query", query).
		Interface("parameters", queryParams).
		Msg("SQL sorgusu hazırlandı")

	// Asıl sorguyu çalıştır
	rows, err := s.db.QueryContext(ctx, query, queryParams...)
	if err != nil {
		logger.Error().
			Err(err).
			Str("agent_id", req.AgentId).
			Str("query", query).
			Interface("params", queryParams).
			Msg("Job sorgusu çalıştırılamadı")
		return nil, fmt.Errorf("job sorgusu çalıştırılamadı: %v", err)
	}
	defer rows.Close()

	// Sonuçları işle
	var jobs []*pb.Job
	for rows.Next() {
		var (
			jobID        string
			jobType      string
			jobStatus    string
			agentID      string
			createdAt    time.Time
			updatedAt    time.Time
			errorMessage sql.NullString
			parameters   []byte // JSON formatında
			result       sql.NullString
		)

		// Satırı oku
		if err := rows.Scan(
			&jobID, &jobType, &jobStatus, &agentID,
			&createdAt, &updatedAt, &errorMessage,
			&parameters, &result,
		); err != nil {
			logger.Error().
				Err(err).
				Msg("Job kaydı okunamadı")
			continue
		}

		// Parameters JSON'ı çözümle
		var paramsMap map[string]string
		if err := json.Unmarshal(parameters, &paramsMap); err != nil {
			logger.Error().
				Err(err).
				Str("job_id", jobID).
				Msg("Job parametreleri çözümlenemedi")
			paramsMap = make(map[string]string) // Boş map kullan
		}

		// JobType ve JobStatus enum değerlerini çözümle
		jobTypeEnum, ok := pb.JobType_value[jobType]
		if !ok {
			logger.Warn().
				Str("job_type", jobType).
				Str("job_id", jobID).
				Msg("Bilinmeyen job tipi, varsayılan olarak JOB_TYPE_UNKNOWN kullanılıyor")
			jobTypeEnum = int32(pb.JobType_JOB_TYPE_UNKNOWN)
		}

		jobStatusEnum, ok := pb.JobStatus_value[jobStatus]
		if !ok {
			logger.Warn().
				Str("job_status", jobStatus).
				Str("job_id", jobID).
				Msg("Bilinmeyen job durumu, varsayılan olarak JOB_STATUS_UNKNOWN kullanılıyor")
			jobStatusEnum = int32(pb.JobStatus_JOB_STATUS_UNKNOWN)
		}

		// pb.Job nesnesi oluştur
		job := &pb.Job{
			JobId:      jobID,
			Type:       pb.JobType(jobTypeEnum),
			Status:     pb.JobStatus(jobStatusEnum),
			AgentId:    agentID,
			CreatedAt:  timestamppb.New(createdAt),
			UpdatedAt:  timestamppb.New(updatedAt),
			Parameters: paramsMap,
		}

		// Null olabilecek alanları işle
		if errorMessage.Valid {
			job.ErrorMessage = errorMessage.String
		}

		if result.Valid {
			job.Result = result.String
		}

		// Job'ı listeye ekle
		jobs = append(jobs, job)
	}

	// Rows.Err() kontrolü
	if err := rows.Err(); err != nil {
		logger.Error().
			Err(err).
			Str("agent_id", req.AgentId).
			Msg("Job kayıtları okunurken hata")
		return nil, fmt.Errorf("job kayıtları okunurken hata: %v", err)
	}

	logger.Debug().
		Int("returned_jobs", len(jobs)).
		Int64("total_jobs", int64(total)).
		Str("agent_id", req.AgentId).
		Msg("Veritabanından job kayıtları döndürülüyor")

	return &pb.ListJobsResponse{
		Jobs:  jobs,
		Total: total,
	}, nil
}

// saveJobToDatabase, job'ı veritabanına kaydeder
func (s *Server) saveJobToDatabase(ctx context.Context, job *pb.Job) error {
	query := `
		INSERT INTO jobs (
			job_id,
			type,
			status,
			agent_id,
			created_at,
			updated_at,
			error_message,
			parameters,
			result
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	// map[string]string tipini JSON'a çevir
	paramsJSON, err := json.Marshal(job.Parameters)
	if err != nil {
		return fmt.Errorf("parameters JSON'a çevrilemedi: %v", err)
	}

	_, err = s.db.ExecContext(ctx, query,
		job.JobId,
		job.Type.String(),
		job.Status.String(),
		job.AgentId,
		job.CreatedAt.AsTime(),
		job.UpdatedAt.AsTime(),
		job.ErrorMessage,
		paramsJSON, // JSON formatında parameterler
		job.Result,
	)

	return err
}

// updateJobInDatabase, job'ı veritabanında günceller
func (s *Server) updateJobInDatabase(ctx context.Context, job *pb.Job) error {
	query := `
		UPDATE jobs SET
			status = $1,
			updated_at = $2,
			error_message = $3,
			result = $4
		WHERE job_id = $5
	`

	_, err := s.db.ExecContext(ctx, query,
		job.Status.String(),
		job.UpdatedAt.AsTime(),
		job.ErrorMessage,
		job.Result,
		job.JobId,
	)

	return err
}

// CreateJob, genel bir job oluşturur ve veritabanına kaydeder
func (s *Server) CreateJob(ctx context.Context, job *pb.Job) error {
	// Job'ı memory'de kaydet
	s.jobMu.Lock()
	s.jobs[job.JobId] = job
	s.jobMu.Unlock()

	// Veritabanına kaydet
	if err := s.saveJobToDatabase(ctx, job); err != nil {
		return err
	}

	// Job tipi ve parametrelere göre uygun işlemi başlat
	go func() {
		// Job durumunu çalışıyor olarak güncelle
		job.Status = pb.JobStatus_JOB_STATUS_RUNNING
		job.UpdatedAt = timestamppb.Now()
		s.updateJobInDatabase(context.Background(), job)

		// İlgili agent'ı bul
		s.mu.RLock()
		agent, exists := s.agents[job.AgentId]
		s.mu.RUnlock()

		if !exists {
			job.Status = pb.JobStatus_JOB_STATUS_FAILED
			job.ErrorMessage = "Agent bulunamadı"
			job.UpdatedAt = timestamppb.Now()
			s.updateJobInDatabase(context.Background(), job)
			return
		}

		var command string

		// Job tipine göre işlemi belirle
		switch job.Type {
		case pb.JobType_JOB_TYPE_MONGO_PROMOTE_PRIMARY:
			nodeHostname := job.Parameters["node_hostname"]
			replicaSet := job.Parameters["replica_set"]
			nodeStatus := job.Parameters["node_status"]
			if nodeHostname == "" || replicaSet == "" {
				job.Status = pb.JobStatus_JOB_STATUS_FAILED
				job.ErrorMessage = "Eksik parametreler: node_hostname veya replica_set"
				break
			}

			// Node status'a göre komutu belirle
			if nodeStatus == "primary" {
				// Eğer node zaten primary ise, farklı bir komut gönder
				command = fmt.Sprintf("db.isMaster() // Node %s zaten primary", nodeHostname)
			} else {
				// Secondary node'u primary'ye yükselt
				command = fmt.Sprintf("rs.stepDown('%s')", nodeHostname)
			}

		case pb.JobType_JOB_TYPE_POSTGRES_PROMOTE_MASTER:
			// Operation type kontrol et - convert_to_slave ise farklı işlem
			operationType := job.Parameters["operation_type"]
			if operationType == "convert_to_slave" {
				// PostgreSQL convert to slave işlemi
				newMasterHost := job.Parameters["new_master_host"]
				newMasterPort := job.Parameters["new_master_port"]
				dataDir := job.Parameters["data_directory"]
				replUser := job.Parameters["replication_user"]
				replPass := job.Parameters["replication_password"]

				if newMasterHost == "" || dataDir == "" || replUser == "" {
					job.Status = pb.JobStatus_JOB_STATUS_FAILED
					job.ErrorMessage = "Eksik parametreler: new_master_host, data_directory veya replication_user"
					break
				}

				if newMasterPort == "" {
					newMasterPort = "5432" // Varsayılan port
				}

				command = fmt.Sprintf("convert_postgres_to_slave|%s|%s|%s|%s|%s",
					newMasterHost, newMasterPort, dataDir, replUser, replPass)
			} else {
				// Normal promotion işlemi
				dataDir := job.Parameters["data_directory"]
				if dataDir == "" {
					job.Status = pb.JobStatus_JOB_STATUS_FAILED
					job.ErrorMessage = "Eksik parametre: data_directory"
					break
				}
				command = fmt.Sprintf("pg_ctl promote -D %s", dataDir)
			}

		case pb.JobType_JOB_TYPE_POSTGRES_CONVERT_TO_SLAVE:
			// PostgreSQL convert to slave işlemi
			newMasterHost := job.Parameters["new_master_host"]
			newMasterIp := job.Parameters["new_master_ip"]
			newMasterPort := job.Parameters["new_master_port"]
			dataDir := job.Parameters["data_directory"]
			coordinationJobId := job.Parameters["coordination_job_id"]
			oldMasterHost := job.Parameters["old_master_host"]

			if newMasterHost == "" || dataDir == "" {
				job.Status = pb.JobStatus_JOB_STATUS_FAILED
				job.ErrorMessage = "Eksik parametreler: new_master_host veya data_directory"
				break
			}

			if newMasterPort == "" {
				newMasterPort = "5432" // Varsayılan port
			}
			if newMasterIp == "" {
				newMasterIp = newMasterHost // Fallback: hostname'i IP olarak kullan
			}

			// Yeni komut formatı
			// NOT: Replication bilgileri artık agent config'inden alınacak (güvenlik)
			command = fmt.Sprintf("convert_postgres_to_slave|%s|%s|%s|%s|%s|%s",
				newMasterHost,     // new_master_host
				newMasterIp,       // new_master_ip
				newMasterPort,     // new_master_port (STRING)
				dataDir,           // data_dir
				coordinationJobId, // coordination_job_id (opsiyonel)
				oldMasterHost)     // old_master_host (opsiyonel)

		default:
			job.Status = pb.JobStatus_JOB_STATUS_FAILED
			job.ErrorMessage = fmt.Sprintf("Desteklenmeyen job tipi: %s", job.Type.String())
		}

		if job.Status == pb.JobStatus_JOB_STATUS_FAILED {
			job.UpdatedAt = timestamppb.Now()
			s.updateJobInDatabase(context.Background(), job)
			return
		}

		// 🔧 FIX: Timeout ile Stream.Send() çağrısı
		sendCtx, sendCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer sendCancel()

		// Channel kullanarak timeout kontrollü gönderme
		sendDone := make(chan error, 1)
		go func() {
			sendErr := agent.Stream.Send(&pb.ServerMessage{
				Payload: &pb.ServerMessage_Query{
					Query: &pb.Query{
						QueryId: job.JobId,
						Command: command,
					},
				},
			})
			sendDone <- sendErr
		}()

		// Timeout veya başarı durumunu bekle
		select {
		case err := <-sendDone:
			if err != nil {
				job.Status = pb.JobStatus_JOB_STATUS_FAILED
				job.ErrorMessage = fmt.Sprintf("Agent'a istek gönderilemedi: %v", err)
			} else {
				job.Status = pb.JobStatus_JOB_STATUS_COMPLETED
				job.Result = "Job request sent successfully"
			}
		case <-sendCtx.Done():
			logger.Error().
				Str("job_id", job.JobId).
				Str("agent_id", job.AgentId).
				Dur("timeout", 10*time.Second).
				Msg("Timeout: CreateJob agent'a gönderilemedi")
			job.Status = pb.JobStatus_JOB_STATUS_FAILED
			job.ErrorMessage = "Timeout: Agent'a istek gönderilemedi (10s timeout)"
		}

		job.UpdatedAt = timestamppb.Now()

		// Database update için de timeout context kullan
		dbCtx, dbCancel := context.WithTimeout(context.Background(), 5*time.Second)
		s.updateJobInDatabase(dbCtx, job)
		dbCancel()
	}()

	return nil
}

// FreezeMongoSecondary, MongoDB secondary node'larını belirli bir süre için dondurur
func (s *Server) FreezeMongoSecondary(ctx context.Context, req *pb.MongoFreezeSecondaryRequest) (*pb.MongoFreezeSecondaryResponse, error) {
	// Job oluştur
	job := &pb.Job{
		JobId:     req.JobId,
		Type:      pb.JobType_JOB_TYPE_MONGO_FREEZE_SECONDARY,
		Status:    pb.JobStatus_JOB_STATUS_PENDING,
		AgentId:   req.AgentId,
		CreatedAt: timestamppb.Now(),
		UpdatedAt: timestamppb.Now(),
		Parameters: map[string]string{
			"node_hostname": req.NodeHostname,
			"port":          fmt.Sprintf("%d", req.Port),
			"replica_set":   req.ReplicaSet,
			"seconds":       fmt.Sprintf("%d", req.Seconds),
		},
	}

	// Job'ı kaydet
	s.jobMu.Lock()
	s.jobs[job.JobId] = job
	s.jobMu.Unlock()

	// İlgili agent'ı bul
	s.mu.RLock()
	agent, exists := s.agents[req.AgentId]
	s.mu.RUnlock()

	if !exists {
		job.Status = pb.JobStatus_JOB_STATUS_FAILED
		job.ErrorMessage = "Agent bulunamadı"
		job.UpdatedAt = timestamppb.Now()
		return &pb.MongoFreezeSecondaryResponse{
			JobId:        job.JobId,
			Status:       job.Status,
			ErrorMessage: job.ErrorMessage,
		}, fmt.Errorf("agent bulunamadı: %s", req.AgentId)
	}

	// Job'ı veritabanına kaydet
	if err := s.saveJobToDatabase(ctx, job); err != nil {
		job.Status = pb.JobStatus_JOB_STATUS_FAILED
		job.ErrorMessage = fmt.Sprintf("Job veritabanına kaydedilemedi: %v", err)
		job.UpdatedAt = timestamppb.Now()
		return &pb.MongoFreezeSecondaryResponse{
			JobId:        job.JobId,
			Status:       job.Status,
			ErrorMessage: job.ErrorMessage,
		}, err
	}

	// Job'ı çalıştır
	go func() {
		job.Status = pb.JobStatus_JOB_STATUS_RUNNING
		job.UpdatedAt = timestamppb.Now()
		s.updateJobInDatabase(context.Background(), job)

		// Seconds parametresini kontrol et, varsayılan 60
		seconds := 60
		if req.Seconds > 0 {
			seconds = int(req.Seconds)
		}

		// rs.freeze() komutu oluştur
		command := fmt.Sprintf("rs.freeze(%d)", seconds)

		// 🔧 FIX: Timeout ile Stream.Send() çağrısı
		sendCtx, sendCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer sendCancel()

		// Channel kullanarak timeout kontrollü gönderme
		sendDone := make(chan error, 1)
		go func() {
			sendErr := agent.Stream.Send(&pb.ServerMessage{
				Payload: &pb.ServerMessage_Query{
					Query: &pb.Query{
						QueryId: job.JobId,
						Command: command,
					},
				},
			})
			sendDone <- sendErr
		}()

		// Timeout veya başarı durumunu bekle
		select {
		case err := <-sendDone:
			if err != nil {
				job.Status = pb.JobStatus_JOB_STATUS_FAILED
				job.ErrorMessage = fmt.Sprintf("Agent'a istek gönderilemedi: %v", err)
			} else {
				job.Status = pb.JobStatus_JOB_STATUS_COMPLETED
				job.Result = fmt.Sprintf("MongoDB node %s successfully frozen for %d seconds", req.NodeHostname, seconds)
			}
		case <-sendCtx.Done():
			logger.Error().
				Str("job_id", job.JobId).
				Str("node_hostname", req.NodeHostname).
				Dur("timeout", 10*time.Second).
				Msg("Timeout: FreezeMongoSecondary agent'a gönderilemedi")
			job.Status = pb.JobStatus_JOB_STATUS_FAILED
			job.ErrorMessage = "Timeout: Agent'a istek gönderilemedi (10s timeout)"
		}

		job.UpdatedAt = timestamppb.Now()

		// Database update için de timeout context kullan
		dbCtx, dbCancel := context.WithTimeout(context.Background(), 5*time.Second)
		s.updateJobInDatabase(dbCtx, job)
		dbCancel()
	}()

	return &pb.MongoFreezeSecondaryResponse{
		JobId:  job.JobId,
		Status: pb.JobStatus_JOB_STATUS_PENDING,
	}, nil
}

// ExplainQuery, PostgreSQL sorgu planını EXPLAIN ANALYZE kullanarak getirir
func (s *Server) ExplainQuery(ctx context.Context, req *pb.ExplainQueryRequest) (*pb.ExplainQueryResponse, error) {
	logger.Info().
		Str("agent_id", req.AgentId).
		Str("database", req.Database).
		Msg("ExplainQuery metodu çağrıldı")

	// Agent ID'yi kontrol et
	agentID := req.AgentId
	if agentID == "" {
		return &pb.ExplainQueryResponse{
			Status:       "error",
			ErrorMessage: "agent_id boş olamaz",
		}, fmt.Errorf("agent_id boş olamaz")
	}

	// Sorguyu kontrol et
	query := req.Query
	if query == "" {
		return &pb.ExplainQueryResponse{
			Status:       "error",
			ErrorMessage: "sorgu boş olamaz",
		}, fmt.Errorf("sorgu boş olamaz")
	}

	// EXPLAIN ANALYZE ile sorguyu çevrele
	explainQuery := fmt.Sprintf("EXPLAIN (ANALYZE true, BUFFERS true, COSTS true, TIMING true, VERBOSE true, FORMAT TEXT) %s", query)

	// Unique bir sorgu ID'si oluştur
	queryID := fmt.Sprintf("explain_%d", time.Now().UnixNano())

	// Agent'a sorguyu gönder ve cevabı al
	result, err := s.SendQuery(ctx, agentID, queryID, explainQuery, req.Database)
	if err != nil {
		logger.Error().
			Err(err).
			Str("agent_id", agentID).
			Str("database", req.Database).
			Str("query_id", queryID).
			Msg("Sorgu planı alınırken hata")
		return &pb.ExplainQueryResponse{
			Status:       "error",
			ErrorMessage: fmt.Sprintf("Sorgu planı alınırken hata: %v", err),
		}, err
	}

	// Sorgu sonucunu döndür
	if result.Result != nil {
		logger.Debug().
			Str("type_url", result.Result.TypeUrl).
			Str("query_id", queryID).
			Msg("Sorgu sonucu alındı")

		// Sonucu okunabilir formata dönüştür
		var resultStruct structpb.Struct
		if err := result.Result.UnmarshalTo(&resultStruct); err != nil {
			logger.Debug().
				Err(err).
				Str("query_id", queryID).
				Msg("Sonuç structpb.Struct'a dönüştürülürken hata")

			// Farklı bir yöntem deneyelim - doğrudan JSON string'e çevirmeyi deneyelim
			resultStr := string(result.Result.Value)
			if len(resultStr) > 0 {
				logger.Debug().
					Int("result_size", len(resultStr)).
					Str("query_id", queryID).
					Msg("Result.Value doğrudan string olarak kullanılıyor")
				return &pb.ExplainQueryResponse{
					Status: "success",
					Plan:   resultStr,
				}, nil
			}

			return &pb.ExplainQueryResponse{
				Status:       "error",
				ErrorMessage: fmt.Sprintf("Sonuç dönüştürülürken hata: %v", err),
			}, err
		}

		// Struct'ı doğrudan JSON string'e dönüştür
		resultMap := resultStruct.AsMap()
		resultBytes, err := json.Marshal(resultMap)
		if err != nil {
			logger.Error().
				Err(err).
				Str("agent_id", req.AgentId).
				Str("database", req.Database).
				Msg("ExplainQuery JSON dönüştürme hatası")
			return &pb.ExplainQueryResponse{
				Status:       "error",
				ErrorMessage: fmt.Sprintf("JSON dönüştürme hatası: %v", err),
			}, err
		}

		// Eğer resultMap içinde özellikle "result" veya "explain" anahtarı varsa,
		// bunları kullanarak okunabilir bir açıklama formatı oluştur
		var planText string
		if explainResult, ok := resultMap["result"]; ok {
			// Sonuç "result" anahtarında ise
			if explainStr, ok := explainResult.(string); ok {
				planText = explainStr
			} else {
				// Eğer doğrudan string değilse JSON olarak dönüştür
				if explainBytes, err := json.MarshalIndent(explainResult, "", "  "); err == nil {
					planText = string(explainBytes)
				}
			}
		} else if explainResult, ok := resultMap["explain"]; ok {
			// Sonuç "explain" anahtarında ise
			if explainStr, ok := explainResult.(string); ok {
				planText = explainStr
			} else {
				// Eğer doğrudan string değilse JSON olarak dönüştür
				if explainBytes, err := json.MarshalIndent(explainResult, "", "  "); err == nil {
					planText = string(explainBytes)
				}
			}
		}

		// Eğer özel format bulunamadıysa, tüm JSON formatını kullan
		if planText == "" {
			planText = string(resultBytes)
		}

		// JSON string'i doğrudan plan alanında kullan
		return &pb.ExplainQueryResponse{
			Status: "success",
			Plan:   planText,
		}, nil
	}

	// Sonuç boş ise hata döndür
	return &pb.ExplainQueryResponse{
		Status:       "error",
		ErrorMessage: "Sorgu planı alınamadı",
	}, fmt.Errorf("sorgu planı alınamadı")
}

// ExplainMongoQuery, MongoDB sorgu planını explain() kullanarak getirir
func (s *Server) ExplainMongoQuery(ctx context.Context, req *pb.ExplainQueryRequest) (*pb.ExplainQueryResponse, error) {
	logger.Info().
		Str("agent_id", req.AgentId).
		Str("database", req.Database).
		Msg("ExplainMongoQuery çağrıldı")

	// Ham sorguyu loglayalım
	logger.Debug().
		Str("raw_query", req.Query).
		Msg("Ham sorgu (JSON)")

	// Agent ID'yi kontrol et
	agentID := req.AgentId
	if agentID == "" {
		return &pb.ExplainQueryResponse{
			Status:       "error",
			ErrorMessage: "agent_id boş olamaz",
		}, fmt.Errorf("agent_id boş olamaz")
	}

	// Sorguyu kontrol et
	query := req.Query
	if query == "" {
		return &pb.ExplainQueryResponse{
			Status:       "error",
			ErrorMessage: "sorgu boş olamaz",
		}, fmt.Errorf("sorgu boş olamaz")
	}

	// Veritabanı adını kontrol et
	database := req.Database
	if database == "" {
		return &pb.ExplainQueryResponse{
			Status:       "error",
			ErrorMessage: "database boş olamaz",
		}, fmt.Errorf("database boş olamaz")
	}

	// Sorguyu MongoDB explain komutu olarak gönder
	// Yeni protokol formatı: MONGO_EXPLAIN|<database>|<query_json>
	explainCommand := fmt.Sprintf("MONGO_EXPLAIN|%s|%s", database, query)

	logger.Debug().
		Str("protocol_format", "MONGO_EXPLAIN|<database>|<query_json>").
		Msg("MongoDB Explain protokol formatı")
	logger.Debug().
		Int("command_length", len(explainCommand)).
		Msg("Hazırlanan sorgu komut uzunluğu")

	// Sorgunun ilk kısmını loglayalım, çok uzunsa sadece başını
	if len(query) > 500 {
		logger.Debug().
			Str("query_preview", query[:500]+"...").
			Int("total_length", len(query)).
			Msg("Sorgu (ilk 500 karakter)")
	} else {
		logger.Debug().
			Str("query", query).
			Msg("Sorgu")
	}

	// Unique bir sorgu ID'si oluştur
	queryID := fmt.Sprintf("mongo_explain_%d", time.Now().UnixNano())

	// Uzun işlem için timeout süresini artır (60 saniye)
	longCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// Agent'a sorguyu gönder ve cevabı al
	logger.Debug().
		Str("agent_id", agentID).
		Str("query_id", queryID).
		Msg("MongoDB sorgusu agent'a gönderiliyor")
	// Burada database parametresini boş geçiyoruz çünkü zaten explainCommand içinde belirttik
	result, err := s.SendQuery(longCtx, agentID, queryID, explainCommand, "")
	if err != nil {
		logger.Error().
			Err(err).
			Str("agent_id", agentID).
			Str("query_id", queryID).
			Str("database", req.Database).
			Msg("MongoDB sorgu planı alınırken hata")
		errMsg := err.Error()
		if err == context.DeadlineExceeded {
			errMsg = "sorgu zaman aşımına uğradı"
		}
		return &pb.ExplainQueryResponse{
			Status:       "error",
			ErrorMessage: fmt.Sprintf("MongoDB sorgu planı alınırken bir hata oluştu: %s", errMsg),
		}, err
	}

	// Sorgu sonucunu döndür
	if result.Result != nil {
		logger.Debug().
			Str("type_url", result.Result.TypeUrl).
			Str("query_id", queryID).
			Msg("MongoDB sorgu planı alındı")

		// Agent'tan gelen ham veriyi direkt kullan
		var rawResult string

		// TypeUrl'e göre işlem yap - direkt ham veriyi çıkarmaya çalış
		if result.Result.TypeUrl == "type.googleapis.com/google.protobuf.Value" {
			// Value tipinde olanlar için direkt string değerini kullan
			rawResult = string(result.Result.Value)
			logger.Debug().
				Int("result_length", len(rawResult)).
				Str("query_id", queryID).
				Msg("Value tipi veri direkt string olarak kullanıldı")
		} else {
			// Struct veya diğer tipler için unmarshalling yapılmalı
			var resultStruct structpb.Struct
			if err := result.Result.UnmarshalTo(&resultStruct); err != nil {
				logger.Error().
					Err(err).
					Str("query_id", queryID).
					Msg("MongoDB sonucu ayrıştırılamadı")
				return &pb.ExplainQueryResponse{
					Status:       "error",
					ErrorMessage: fmt.Sprintf("MongoDB sorgu planı ayrıştırılamadı: %v", err),
				}, err
			}

			// Struct'ı JSON olarak serialize et
			resultMap := resultStruct.AsMap()
			resultBytes, err := json.Marshal(resultMap)
			if err != nil {
				logger.Error().
					Err(err).
					Str("query_id", queryID).
					Msg("MongoDB JSON serileştirilirken hata")
				return &pb.ExplainQueryResponse{
					Status:       "error",
					ErrorMessage: fmt.Sprintf("MongoDB JSON serileştirme hatası: %v", err),
				}, err
			}

			rawResult = string(resultBytes)
		}

		// Yanıtı döndür
		return &pb.ExplainQueryResponse{
			Status: "success",
			Plan:   rawResult,
		}, nil
	}

	// Sonuç boş ise hata döndür
	logger.Error().
		Str("query_id", queryID).
		Str("agent_id", agentID).
		Msg("MongoDB sorgu planı boş sonuç döndü")
	return &pb.ExplainQueryResponse{
		Status:       "error",
		ErrorMessage: "MongoDB sorgu planı alınamadı: Boş sonuç",
	}, fmt.Errorf("MongoDB sorgu planı alınamadı: Boş sonuç")
}

// SendMSSQLInfo, agent'dan gelen MSSQL bilgilerini işler
func (s *Server) SendMSSQLInfo(ctx context.Context, req *pb.MSSQLInfoRequest) (*pb.MSSQLInfoResponse, error) {
	logger.Info().
		Str("cluster", req.MssqlInfo.ClusterName).
		Str("hostname", req.MssqlInfo.Hostname).
		Bool("ha_enabled", req.MssqlInfo.IsHaEnabled).
		Msg("SendMSSQLInfo metodu çağrıldı")

	// Gelen MSSQL bilgilerini logla
	mssqlInfo := req.MssqlInfo
	
	// Agent ID'sini metrik verilerinden çıkar ve agent durumunu active yap
	agentID := fmt.Sprintf("agent_%s", mssqlInfo.Hostname)
	s.UpdateAgentConnectedStatus(agentID)

	// AlwaysOn bilgilerini logla
	if mssqlInfo.IsHaEnabled && mssqlInfo.AlwaysOnMetrics != nil {
		alwaysOn := mssqlInfo.AlwaysOnMetrics
		logger.Info().
			Str("cluster_name", alwaysOn.ClusterName).
			Str("health_state", alwaysOn.HealthState).
			Str("operational_state", alwaysOn.OperationalState).
			Str("primary_replica", alwaysOn.PrimaryReplica).
			Str("local_role", alwaysOn.LocalRole).
			Str("sync_mode", alwaysOn.SynchronizationMode).
			Int64("replication_lag_ms", alwaysOn.ReplicationLagMs).
			Int64("log_send_queue_kb", alwaysOn.LogSendQueueKb).
			Int64("redo_queue_kb", alwaysOn.RedoQueueKb).
			Msg("AlwaysOn Cluster bilgileri")

		if len(alwaysOn.Replicas) > 0 {
			logger.Debug().
				Int("replica_count", len(alwaysOn.Replicas)).
				Msg("AlwaysOn Replicas")
			for i, replica := range alwaysOn.Replicas {
				logger.Debug().
					Int("replica_index", i+1).
					Str("replica_name", replica.ReplicaName).
					Str("role", replica.Role).
					Str("connection_state", replica.ConnectionState).
					Msg("AlwaysOn Replica")
			}
		}

		if len(alwaysOn.Listeners) > 0 {
			logger.Debug().
				Int("listener_count", len(alwaysOn.Listeners)).
				Msg("AlwaysOn Listeners")
			for i, listener := range alwaysOn.Listeners {
				logger.Debug().
					Int("listener_index", i+1).
					Str("listener_name", listener.ListenerName).
					Int32("port", listener.Port).
					Str("state", listener.ListenerState).
					Msg("AlwaysOn Listener")
			}
		}
	} else if mssqlInfo.IsHaEnabled {
		logger.Warn().
			Str("hostname", mssqlInfo.Hostname).
			Msg("HA enabled but AlwaysOn metrics not available")
	}

	// Veritabanına kaydetme işlemi
	err := s.saveMSSQLInfoToDatabase(ctx, mssqlInfo)
	if err != nil {
		logger.Error().
			Err(err).
			Str("cluster", mssqlInfo.ClusterName).
			Str("hostname", mssqlInfo.Hostname).
			Msg("MSSQL bilgileri veritabanına kaydedilemedi")
		return &pb.MSSQLInfoResponse{
			Status: "error",
		}, nil
	}

	logger.Info().
		Str("cluster", mssqlInfo.ClusterName).
		Str("hostname", mssqlInfo.Hostname).
		Msg("MSSQL bilgileri başarıyla işlendi ve kaydedildi")

	return &pb.MSSQLInfoResponse{
		Status: "success",
	}, nil
}

// MSSQL bilgilerini veritabanına kaydetmek için yardımcı fonksiyon
func (s *Server) saveMSSQLInfoToDatabase(ctx context.Context, mssqlInfo *pb.MSSQLInfo) error {
	// Önce mevcut kaydı kontrol et
	var existingData []byte
	var id int

	checkQuery := `
		SELECT id, jsondata FROM public.mssql_data 
		WHERE clustername = $1 
		ORDER BY id DESC LIMIT 1
	`

	err := s.db.QueryRowContext(ctx, checkQuery, mssqlInfo.ClusterName).Scan(&id, &existingData)

	// Yeni node verisi
	mssqlData := map[string]interface{}{
		"ClusterName": mssqlInfo.ClusterName,
		"Location":    mssqlInfo.Location,
		"FDPercent":   mssqlInfo.FdPercent,
		"FreeDisk":    mssqlInfo.FreeDisk,
		"Hostname":    mssqlInfo.Hostname,
		"IP":          mssqlInfo.Ip,
		"NodeStatus":  mssqlInfo.NodeStatus,
		"Status":      mssqlInfo.Status,
		"Version":     mssqlInfo.Version,
		"Instance":    mssqlInfo.Instance,
		"Port":        mssqlInfo.Port,
		"TotalVCPU":   mssqlInfo.TotalVcpu,
		"TotalMemory": mssqlInfo.TotalMemory,
		"ConfigPath":  mssqlInfo.ConfigPath,
		"Database":    mssqlInfo.Database,
		"IsHAEnabled": mssqlInfo.IsHaEnabled,
		"HARole":      mssqlInfo.HaRole,
		"Edition":     mssqlInfo.Edition,
	}

	// AlwaysOn bilgilerini ekle (eğer mevcutsa)
	if mssqlInfo.IsHaEnabled && mssqlInfo.AlwaysOnMetrics != nil {
		alwaysOnData := map[string]interface{}{
			"ClusterName":         mssqlInfo.AlwaysOnMetrics.ClusterName,
			"HealthState":         mssqlInfo.AlwaysOnMetrics.HealthState,
			"OperationalState":    mssqlInfo.AlwaysOnMetrics.OperationalState,
			"SynchronizationMode": mssqlInfo.AlwaysOnMetrics.SynchronizationMode,
			"FailoverMode":        mssqlInfo.AlwaysOnMetrics.FailoverMode,
			"PrimaryReplica":      mssqlInfo.AlwaysOnMetrics.PrimaryReplica,
			"LocalRole":           mssqlInfo.AlwaysOnMetrics.LocalRole,
			"LastFailoverTime":    mssqlInfo.AlwaysOnMetrics.LastFailoverTime,
			"ReplicationLagMs":    mssqlInfo.AlwaysOnMetrics.ReplicationLagMs,
			"LogSendQueueKb":      mssqlInfo.AlwaysOnMetrics.LogSendQueueKb,
			"RedoQueueKb":         mssqlInfo.AlwaysOnMetrics.RedoQueueKb,
		}

		// Replica bilgilerini ekle
		if len(mssqlInfo.AlwaysOnMetrics.Replicas) > 0 {
			replicas := make([]map[string]interface{}, len(mssqlInfo.AlwaysOnMetrics.Replicas))
			for i, replica := range mssqlInfo.AlwaysOnMetrics.Replicas {
				replicas[i] = map[string]interface{}{
					"ReplicaName":         replica.ReplicaName,
					"Role":                replica.Role,
					"ConnectionState":     replica.ConnectionState,
					"SynchronizationMode": replica.SynchronizationMode,
					"FailoverMode":        replica.FailoverMode,
					"AvailabilityMode":    replica.AvailabilityMode,
					"JoinState":           replica.JoinState,
					"ConnectedState":      replica.ConnectedState,
					"SuspendReason":       replica.SuspendReason,
				}
			}
			alwaysOnData["Replicas"] = replicas
		}

		// Database bilgilerini ekle
		if len(mssqlInfo.AlwaysOnMetrics.Databases) > 0 {
			databases := make([]map[string]interface{}, len(mssqlInfo.AlwaysOnMetrics.Databases))
			for i, db := range mssqlInfo.AlwaysOnMetrics.Databases {
				databases[i] = map[string]interface{}{
					"DatabaseName":         db.DatabaseName,
					"ReplicaName":          db.ReplicaName,
					"SynchronizationState": db.SynchronizationState,
					"SuspendReason":        db.SuspendReason,
					"LastSentTime":         db.LastSentTime,
					"LastReceivedTime":     db.LastReceivedTime,
					"LastHardenedTime":     db.LastHardenedTime,
					"LastRedoneTime":       db.LastRedoneTime,
					"LogSendQueueKb":       db.LogSendQueueKb,
					"LogSendRateKbPerSec":  db.LogSendRateKbPerSec,
					"RedoQueueKb":          db.RedoQueueKb,
					"RedoRateKbPerSec":     db.RedoRateKbPerSec,
					"EndOfLogLsn":          db.EndOfLogLsn,
					"RecoveryLsn":          db.RecoveryLsn,
					"TruncationLsn":        db.TruncationLsn,
					"LastCommitLsn":        db.LastCommitLsn,
					"LastCommitTime":       db.LastCommitTime,
				}
			}
			alwaysOnData["Databases"] = databases
		}

		// Listener bilgilerini ekle
		if len(mssqlInfo.AlwaysOnMetrics.Listeners) > 0 {
			listeners := make([]map[string]interface{}, len(mssqlInfo.AlwaysOnMetrics.Listeners))
			for i, listener := range mssqlInfo.AlwaysOnMetrics.Listeners {
				listeners[i] = map[string]interface{}{
					"ListenerName":  listener.ListenerName,
					"IpAddresses":   listener.IpAddresses,
					"Port":          listener.Port,
					"SubnetMask":    listener.SubnetMask,
					"ListenerState": listener.ListenerState,
					"DnsName":       listener.DnsName,
				}
			}
			alwaysOnData["Listeners"] = listeners
		}

		mssqlData["AlwaysOnMetrics"] = alwaysOnData
		logger.Debug().
			Str("hostname", mssqlInfo.Hostname).
			Str("cluster", mssqlInfo.ClusterName).
			Msg("AlwaysOn metrics veritabanına kaydedildi")
	}

	var jsonData []byte

	// Hata kontrolünü düzgün yap
	if err == nil {
		// Mevcut kayıt var, güncelle
		logger.Debug().
			Str("cluster", mssqlInfo.ClusterName).
			Int("id", id).
			Msg("MSSQL cluster için mevcut kayıt bulundu, güncelleniyor")

		var existingJSON map[string][]interface{}
		if err := json.Unmarshal(existingData, &existingJSON); err != nil {
			logger.Error().
				Err(err).
				Str("cluster", mssqlInfo.ClusterName).
				Msg("MSSQL mevcut JSON ayrıştırma hatası")
			return err
		}

		// Cluster array'ini al
		clusterData, ok := existingJSON[mssqlInfo.ClusterName]
		if !ok {
			// Eğer cluster verisi yoksa yeni oluştur
			clusterData = []interface{}{}
		}

		// Node'u bul ve güncelle
		nodeFound := false
		nodeChanged := false
		for i, node := range clusterData {
			nodeMap, ok := node.(map[string]interface{})
			if !ok {
				continue
			}

			// Hostname ve IP ile node eşleşmesi kontrol et
			if nodeMap["Hostname"] == mssqlInfo.Hostname && nodeMap["IP"] == mssqlInfo.Ip {
				// Sadece değişen alanları güncelle
				nodeFound = true

				// Değişiklikleri takip et
				for key, newValue := range mssqlData {
					currentValue, exists := nodeMap[key]
					var hasChanged bool

					if !exists {
						// Değer mevcut değil, yeni alan ekleniyor
						hasChanged = true
						logger.Debug().
							Str("hostname", mssqlInfo.Hostname).
							Str("field", key).
							Msg("MSSQL node'da yeni alan eklendi")
					} else {
						// Mevcut değer ile yeni değeri karşılaştır
						// Numeric değerler için özel karşılaştırma yap
						hasChanged = !compareValues(currentValue, newValue)
						if hasChanged {
							if key == "AlwaysOnMetrics" {
								logger.Debug().
									Str("hostname", mssqlInfo.Hostname).
									Msg("MSSQL node'da AlwaysOn metrics güncellendi")
							} else {
								logger.Debug().
									Str("hostname", mssqlInfo.Hostname).
									Str("field", key).
									Interface("old_value", currentValue).
									Interface("new_value", newValue).
									Msg("MSSQL node'da değişiklik tespit edildi")
							}
						}
					}

					if hasChanged {
						nodeMap[key] = newValue
						// HARole, Status, AlwaysOnMetrics gibi önemli bir değişiklik varsa işaretle
						if key == "HARole" || key == "Status" || key == "NodeStatus" || key == "FreeDisk" || key == "AlwaysOnMetrics" {
							nodeChanged = true
						}
					}
				}

				clusterData[i] = nodeMap
				break
			}
		}

		// Eğer node bulunamadıysa yeni ekle
		if !nodeFound {
			clusterData = append(clusterData, mssqlData)
			nodeChanged = true
			logger.Info().
				Str("hostname", mssqlInfo.Hostname).
				Str("cluster", mssqlInfo.ClusterName).
				Msg("Yeni MSSQL node eklendi")
		}

		// Eğer önemli bir değişiklik yoksa veritabanını güncelleme
		if !nodeChanged {
			logger.Debug().
				Str("hostname", mssqlInfo.Hostname).
				Str("cluster", mssqlInfo.ClusterName).
				Msg("MSSQL node'da önemli bir değişiklik yok, güncelleme yapılmadı")
			return nil
		}

		existingJSON[mssqlInfo.ClusterName] = clusterData

		// JSON'ı güncelle
		jsonData, err = json.Marshal(existingJSON)
		if err != nil {
			logger.Error().
				Err(err).
				Str("cluster", mssqlInfo.ClusterName).
				Msg("MSSQL JSON dönüştürme hatası")
			return err
		}

		// Veritabanını güncelle
		updateQuery := `
			UPDATE public.mssql_data 
			SET jsondata = $1, updated_at = CURRENT_TIMESTAMP
			WHERE id = $2
		`

		_, err = s.db.ExecContext(ctx, updateQuery, jsonData, id)
		if err != nil {
			logger.Error().
				Err(err).
				Str("cluster", mssqlInfo.ClusterName).
				Int("id", id).
				Msg("MSSQL veritabanı güncelleme hatası")
			return err
		}

		logger.Info().
			Str("hostname", mssqlInfo.Hostname).
			Str("cluster", mssqlInfo.ClusterName).
			Int("record_id", id).
			Msg("MSSQL node bilgileri başarıyla güncellendi (önemli değişiklik nedeniyle)")
	} else if err == sql.ErrNoRows {
		// Kayıt bulunamadı, yeni kayıt oluştur
		logger.Info().
			Str("cluster", mssqlInfo.ClusterName).
			Str("hostname", mssqlInfo.Hostname).
			Msg("MSSQL cluster için kayıt bulunamadı, yeni kayıt oluşturuluyor")

		outerJSON := map[string][]interface{}{
			mssqlInfo.ClusterName: {mssqlData},
		}

		jsonData, err = json.Marshal(outerJSON)
		if err != nil {
			logger.Error().
				Err(err).
				Str("cluster", mssqlInfo.ClusterName).
				Msg("MSSQL yeni kayıt JSON dönüştürme hatası")
			return err
		}

		insertQuery := `
			INSERT INTO public.mssql_data (
				jsondata, clustername, created_at, updated_at
			) VALUES ($1, $2, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
			ON CONFLICT (clustername) DO UPDATE SET
				jsondata = EXCLUDED.jsondata,
				updated_at = CURRENT_TIMESTAMP
		`

		_, err = s.db.ExecContext(ctx, insertQuery, jsonData, mssqlInfo.ClusterName)
		if err != nil {
			logger.Error().
				Err(err).
				Str("cluster", mssqlInfo.ClusterName).
				Str("hostname", mssqlInfo.Hostname).
				Msg("MSSQL veritabanı ekleme hatası")
			return err
		}

		logger.Info().
			Str("cluster", mssqlInfo.ClusterName).
			Str("hostname", mssqlInfo.Hostname).
			Msg("MSSQL node bilgileri başarıyla veritabanına kaydedildi (yeni kayıt)")
	} else {
		// Başka bir veritabanı hatası oluştu
		logger.Error().
			Err(err).
			Str("cluster", mssqlInfo.ClusterName).
			Str("hostname", mssqlInfo.Hostname).
			Msg("MSSQL cluster kayıt kontrolü sırasında hata")
		return fmt.Errorf("veritabanı kontrol hatası: %v", err)
	}

	return nil
}

// GetStatusMSSQL, MSSQL veritabanından durum bilgilerini çeker
func (s *Server) GetStatusMSSQL(ctx context.Context, _ *structpb.Struct) (*structpb.Value, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT json_agg(sub.jsondata) FROM (SELECT jsondata FROM mssql_data ORDER BY id) AS sub")
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Veritabanı sorgusu başarısız: %v", err)
	}
	defer rows.Close()

	var jsonData []byte
	if rows.Next() {
		err := rows.Scan(&jsonData)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Veri okuma hatası: %v", err)
		}
	}

	// JSON verisini structpb.Value'ya dönüştür
	var jsonValue interface{}
	if err := json.Unmarshal(jsonData, &jsonValue); err != nil {
		return nil, status.Errorf(codes.Internal, "JSON ayrıştırma hatası: %v", err)
	}

	value, err := structpb.NewValue(jsonValue)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Veri dönüştürme hatası: %v", err)
	}

	return value, nil
}

// GetMSSQLBestPracticesAnalysis, MSSQL best practice analizi gerçekleştirir
func (s *Server) GetMSSQLBestPracticesAnalysis(ctx context.Context, agentID, database string) (*pb.BestPracticesAnalysisResponse, error) {
	logger.Info().
		Str("agent_id", agentID).
		Str("database", database).
		Msg("GetMSSQLBestPracticesAnalysis çağrıldı")

	// Agent ID'yi kontrol et
	if agentID == "" {
		return nil, fmt.Errorf("agent_id boş olamaz")
	}

	// BestPracticesAnalysisRequest oluştur
	req := &pb.BestPracticesAnalysisRequest{
		AgentId:      agentID,
		DatabaseName: database,
	}

	// RPC çağrısını yap
	return s.GetBestPracticesAnalysis(ctx, req)
}

// GetBestPracticesAnalysis, MSSQL best practice analizi için gRPC çağrısını yapar
func (s *Server) GetBestPracticesAnalysis(ctx context.Context, req *pb.BestPracticesAnalysisRequest) (*pb.BestPracticesAnalysisResponse, error) {
	agentID := req.AgentId

	// Agent'ın bağlı olup olmadığını kontrol et
	s.mu.RLock()
	agent, agentExists := s.agents[agentID]
	s.mu.RUnlock()

	if !agentExists || agent == nil {
		return nil, fmt.Errorf("agent bulunamadı veya bağlantı kapalı: %s", agentID)
	}

	// Unique bir sorgu ID'si oluştur
	queryID := fmt.Sprintf("mssql_bpa_%d", time.Now().UnixNano())

	// MSSQL_BPA formatında komut oluştur (database parametresi opsiyonel)
	command := "MSSQL_BPA"
	if req.DatabaseName != "" {
		command = fmt.Sprintf("MSSQL_BPA|%s", req.DatabaseName)
	}
	if req.ServerName != "" {
		command = fmt.Sprintf("%s|%s", command, req.ServerName)
	}

	logger.Debug().
		Str("command", command).
		Str("agent_id", agentID).
		Msg("MSSQL Best Practices Analysis komut")

	// Sonuç kanalı oluştur
	resultChan := make(chan *pb.QueryResult, 1)
	s.queryMu.Lock()
	s.queryResult[queryID] = &QueryResponse{
		ResultChan: resultChan,
	}
	s.queryMu.Unlock()

	// Context'in iptal durumunda kaynakları temizle
	defer func() {
		s.queryMu.Lock()
		delete(s.queryResult, queryID)
		s.queryMu.Unlock()
		close(resultChan)
	}()

	// Sorguyu gönder
	err := agent.Stream.Send(&pb.ServerMessage{
		Payload: &pb.ServerMessage_Query{
			Query: &pb.Query{
				QueryId: queryID,
				Command: command,
			},
		},
	})
	if err != nil {
		logger.Error().
			Err(err).
			Str("agent_id", agentID).
			Str("query_id", queryID).
			Msg("MSSQL Best Practices Analysis sorgusu gönderilemedi")
		return nil, fmt.Errorf("sorgu gönderilemedi: %v", err)
	}

	logger.Info().
		Str("agent_id", agentID).
		Str("query_id", queryID).
		Msg("MSSQL Best Practices Analysis sorgusu gönderildi")

	// Cevabı bekle
	select {
	case result := <-resultChan:
		// Sonuç geldi
		if result == nil {
			logger.Error().
				Str("query_id", queryID).
				Str("agent_id", agentID).
				Msg("Best Practices Analysis null sonuç alındı")
			return nil, fmt.Errorf("null sorgu sonucu alındı")
		}

		logger.Debug().
			Str("query_id", queryID).
			Str("type_url", result.Result.TypeUrl).
			Str("agent_id", agentID).
			Msg("Best Practices Analysis yanıt alındı")

		// Sonucu BestPracticesAnalysisResponse'a dönüştür
		response := &pb.BestPracticesAnalysisResponse{
			Status:            "success",
			AnalysisId:        queryID,
			AnalysisTimestamp: time.Now().Format(time.RFC3339),
		}

		// Any tipini çözümle
		if result.Result.TypeUrl == "type.googleapis.com/google.protobuf.Value" {
			// Value tipinde ise, doğrudan JSON string olarak kullan
			response.AnalysisResults = result.Result.Value
			logger.Debug().
				Int("result_size", len(response.AnalysisResults)).
				Str("type", "JSON string").
				Msg("Best Practices Analysis sonucu alındı")
		} else {
			// Struct olarak çözümle
			var resultStruct structpb.Struct
			if err := result.Result.UnmarshalTo(&resultStruct); err != nil {
				logger.Error().
					Err(err).
					Msg("Struct çözümleme hatası")
				return nil, fmt.Errorf("sonuç çözümleme hatası: %v", err)
			}

			// Struct'tan JSON string oluştur
			resultBytes, err := json.Marshal(resultStruct.AsMap())
			if err != nil {
				logger.Error().
					Err(err).
					Msg("JSON dönüştürme hatası")
				return nil, fmt.Errorf("JSON dönüştürme hatası: %v", err)
			}

			response.AnalysisResults = resultBytes
			logger.Debug().
				Int("result_size", len(response.AnalysisResults)).
				Str("type", "Struct->JSON").
				Msg("Best Practices Analysis sonucu alındı")
		}

		return response, nil

	case <-ctx.Done():
		// Context iptal edildi veya zaman aşımına uğradı
		logger.Error().
			Err(ctx.Err()).
			Msg("Best Practices Analysis sonucu beklerken timeout/iptal")
		return nil, ctx.Err()
	}
}

// ReportProcessLogs, agent'lardan gelen işlem loglarını işler
func (s *Server) ReportProcessLogs(ctx context.Context, req *pb.ProcessLogRequest) (*pb.ProcessLogResponse, error) {
	logger.Info().
		Str("agent_id", req.LogUpdate.AgentId).
		Str("process_id", req.LogUpdate.ProcessId).
		Str("process_type", req.LogUpdate.ProcessType).
		Msg("ReportProcessLogs metodu çağrıldı")

	// Gelen log mesajlarını logla
	for _, msg := range req.LogUpdate.LogMessages {
		logger.Debug().
			Str("process_id", req.LogUpdate.ProcessId).
			Str("status", req.LogUpdate.Status).
			Str("message", msg).
			Msg("Process Log")
	}

	// DEBUG: Metadata içeriğini tamamen logla
	if metadata := req.LogUpdate.Metadata; metadata != nil {
		logger.Debug().
			Str("agent_id", req.LogUpdate.AgentId).
			Int("metadata_count", len(metadata)).
			Interface("metadata", metadata).
			Msg("ProcessLoggerHandler metadata içeriği")
	} else {
		logger.Debug().
			Str("agent_id", req.LogUpdate.AgentId).
			Msg("ProcessLoggerHandler - metadata yok")
	}

	// ProcessLoggerHandler'da metadata kontrolü ekle
	if metadata := req.LogUpdate.Metadata; metadata != nil {
		// 1. Failover Coordination Request (Yeni master'dan gelen)
		if isFailoverRequest, exists := metadata["failover_coordination_request"]; exists && isFailoverRequest == "true" {
			logger.Info().
				Str("agent_id", req.LogUpdate.AgentId).
				Str("process_id", req.LogUpdate.ProcessId).
				Int("metadata_count", len(metadata)).
				Msg("Failover koordinasyon talebi algılandı")
			logger.Debug().
				Str("agent_id", req.LogUpdate.AgentId).
				Interface("metadata", metadata).
				Msg("Coordination metadata detayları")

			// 🔧 FIX: Akıllı duplicate coordination prevention check
			coordinationKey := fmt.Sprintf("%s_%s_%s_%s",
				req.LogUpdate.ProcessId,
				metadata["old_master_host"],
				metadata["new_master_host"],
				metadata["action"])

			s.coordinationMu.Lock()
			lastProcessed, alreadyProcessed := s.processedCoordinations[coordinationKey]

			// 🔧 FIX: Reduced timeout from 2 minutes to 30 seconds for faster retry
			// This allows users to retry promotion sooner if the first attempt fails
			if alreadyProcessed && time.Since(lastProcessed) < 30*time.Second {
				s.coordinationMu.Unlock()
				logger.Warn().
					Str("coordination_key", coordinationKey).
					Dur("last_processed_ago", time.Since(lastProcessed).Round(time.Second)).
					Msg("Duplicate coordination request skipped (within 30 seconds)")
				return &pb.ProcessLogResponse{
					Status:  "ok",
					Message: "Process logları başarıyla alındı (duplicate coordination skipped)",
				}, nil
			}

			// Bu coordination'ı işlenmiş olarak işaretle
			s.processedCoordinations[coordinationKey] = time.Now()

			// 🔧 FIX: More aggressive cleanup - every coordination request triggers cleanup
			// This helps prevent memory leaks and stuck coordination states
			go s.cleanupOldCoordinations()

			s.coordinationMu.Unlock()

			logger.Info().
				Str("coordination_key", coordinationKey).
				Bool("was_previously_processed", alreadyProcessed).
				Msg("Coordination request accepted")

			// Koordinasyon işlemini başlat
			logger.Info().
				Str("coordination_key", coordinationKey).
				Str("agent_id", req.LogUpdate.AgentId).
				Msg("Coordination işlemi goroutine'de başlatılıyor")
			go s.handleFailoverCoordination(req.LogUpdate, req.LogUpdate.AgentId)
		}

		// 2. Coordination Completion (Eski master'dan gelen)
		logger.Debug().Msg("Checking for coordination_completion metadata...")
		if coordinationCompletionValue, exists := metadata["coordination_completion"]; exists {
			logger.Debug().
				Str("completion_value", coordinationCompletionValue).
				Bool("exists", exists).
				Msg("coordination_completion metadata found")
			if coordinationCompletionValue == "true" {
				logger.Info().
					Str("agent_id", req.LogUpdate.AgentId).
					Str("process_id", req.LogUpdate.ProcessId).
					Int("metadata_count", len(metadata)).
					Msg("Coordination completion bildirimi algılandı")
				logger.Debug().
					Str("agent_id", req.LogUpdate.AgentId).
					Interface("metadata", metadata).
					Msg("Completion metadata detayları")

				// Coordination completion işlemini handle et
				logger.Debug().Msg("handleCoordinationCompletion fonksiyonu çağrılıyor")
				go s.handleCoordinationCompletion(req.LogUpdate, req.LogUpdate.AgentId)
			} else {
				logger.Debug().
					Str("completion_value", coordinationCompletionValue).
					Msg("coordination_completion metadata var ama 'true' değil")
			}
		} else {
			logger.Debug().Msg("coordination_completion metadata bulunamadı")
		}

		// 3. Coordination Rollback Request (Rollback sırasında gelen)
		if rollbackRequest, exists := metadata["coordination_rollback_request"]; exists && rollbackRequest == "true" {
			logger.Info().
				Str("agent_id", req.LogUpdate.AgentId).
				Str("process_id", req.LogUpdate.ProcessId).
				Msg("Coordination rollback request detected")

			// Rollback parametrelerini al
			targetHost := metadata["rollback_target_host"]
			targetIP := metadata["rollback_target_ip"]
			rollbackAction := metadata["rollback_action"]
			rollbackReason := metadata["rollback_reason"]
			originalMasterHost := metadata["original_master_host"]
			originalMasterIP := metadata["original_master_ip"]

			logger.Info().
				Str("target_host", targetHost).
				Str("target_ip", targetIP).
				Str("rollback_action", rollbackAction).
				Str("rollback_reason", rollbackReason).
				Str("original_master_host", originalMasterHost).
				Str("original_master_ip", originalMasterIP).
				Msg("Coordination rollback parameters")

			// Asenkron olarak koordinasyon rollback'i başlat
			go s.handleCoordinationRollback(targetHost, targetIP, originalMasterHost, originalMasterIP, rollbackReason)
		}
	}

	// Normal process log işleme devam et...
	err := s.saveProcessLogs(ctx, req.LogUpdate)
	if err != nil {
		logger.Error().
			Err(err).
			Str("agent_id", req.LogUpdate.AgentId).
			Str("process_id", req.LogUpdate.ProcessId).
			Msg("Process logları veritabanına kaydedilemedi")
		return &pb.ProcessLogResponse{
			Status:  "error",
			Message: fmt.Sprintf("Veritabanı hatası: %v", err),
		}, nil
	}

	return &pb.ProcessLogResponse{
		Status:  "ok",
		Message: "Process logları başarıyla alındı",
	}, nil
}

// saveProcessLogs, işlem loglarını veritabanına kaydeder
func (s *Server) saveProcessLogs(ctx context.Context, logUpdate *pb.ProcessLogUpdate) error {
	// Veritabanı bağlantısını kontrol et
	if err := s.checkDatabaseConnection(); err != nil {
		return fmt.Errorf("veritabanı bağlantı hatası: %v", err)
	}

	// Metadata'yı JSON formatına dönüştür
	metadataJSON, err := json.Marshal(logUpdate.Metadata)
	if err != nil {
		return fmt.Errorf("metadata JSON dönüştürme hatası: %v", err)
	}

	// Log mesajlarını JSON formatına dönüştür
	logMessagesJSON, err := json.Marshal(logUpdate.LogMessages)
	if err != nil {
		return fmt.Errorf("log mesajları JSON dönüştürme hatası: %v", err)
	}

	// İşlemin var olup olmadığını kontrol et
	var processExists bool
	err = s.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM process_logs 
			WHERE process_id = $1 AND agent_id = $2
		)
	`, logUpdate.ProcessId, logUpdate.AgentId).Scan(&processExists)

	if err != nil {
		return fmt.Errorf("işlem kaydı kontrolü sırasında hata: %v", err)
	}

	if !processExists {
		// İşlem ilk kez kaydediliyor
		logger.Info().
			Str("process_id", logUpdate.ProcessId).
			Str("agent_id", logUpdate.AgentId).
			Msg("Yeni işlem kaydı oluşturuluyor")

		insertQuery := `
			INSERT INTO process_logs (
				process_id,
				agent_id,
				process_type,
				status,
				log_messages,
				elapsed_time_s,
				metadata,
				created_at,
				updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
		`

		_, err = s.db.ExecContext(ctx, insertQuery,
			logUpdate.ProcessId,
			logUpdate.AgentId,
			logUpdate.ProcessType,
			logUpdate.Status,
			logMessagesJSON,
			logUpdate.ElapsedTimeS,
			metadataJSON,
			time.Now(),
		)

		if err != nil {
			return fmt.Errorf("yeni işlem kaydı eklenirken hata: %v", err)
		}
	} else {
		// Mevcut işlem güncelleniyor
		logger.Debug().
			Str("process_id", logUpdate.ProcessId).
			Str("agent_id", logUpdate.AgentId).
			Msg("Mevcut işlem kaydı güncelleniyor")

		// Mevcut log mesajlarını ve status'u al
		var existingLogs []byte
		var currentStatus string
		err = s.db.QueryRowContext(ctx, `
    SELECT log_messages, status FROM process_logs 
    WHERE process_id = $1 AND agent_id = $2
`, logUpdate.ProcessId, logUpdate.AgentId).Scan(&existingLogs, &currentStatus)

		if err != nil {
			return fmt.Errorf("mevcut log mesajları alınırken hata: %v", err)
		}

		// 🔧 FIX: Eğer process zaten 'completed' veya 'failed' durumundaysa,
		// sadece log mesajlarını ekle, status'u değiştirme (race condition koruması)
		finalStatus := logUpdate.Status
		if currentStatus == "completed" || currentStatus == "failed" {
			logger.Debug().
				Str("process_id", logUpdate.ProcessId).
				Str("current_status", currentStatus).
				Str("incoming_status", logUpdate.Status).
				Msg("Process zaten final durumda, status korunuyor")
			finalStatus = currentStatus // Mevcut final status'u koru
		}

		// Mevcut log mesajlarını çözümle
		var existingLogMessages []string
		if err := json.Unmarshal(existingLogs, &existingLogMessages); err != nil {
			return fmt.Errorf("mevcut log mesajları ayrıştırılırken hata: %v", err)
		}

		// Yeni log mesajlarını ekle
		combinedLogMessages := append(existingLogMessages, logUpdate.LogMessages...)

		// Birleştirilmiş log mesajlarını JSON'a dönüştür
		combinedLogsJSON, err := json.Marshal(combinedLogMessages)
		if err != nil {
			return fmt.Errorf("birleştirilmiş log mesajları JSON dönüştürme hatası: %v", err)
		}

		// Güncelleme sorgusunu çalıştır
		updateQuery := `
			UPDATE process_logs SET 
				status = $1,
				log_messages = $2,
				elapsed_time_s = $3,
				metadata = $4,
				updated_at = $5
			WHERE process_id = $6 AND agent_id = $7
		`

		_, err = s.db.ExecContext(ctx, updateQuery,
			finalStatus,
			combinedLogsJSON,
			logUpdate.ElapsedTimeS,
			metadataJSON,
			time.Now(),
			logUpdate.ProcessId,
			logUpdate.AgentId,
		)

		if err != nil {
			return fmt.Errorf("işlem kaydı güncellenirken hata: %v", err)
		}
	}

	// Eğer işlem tamamlandıysa veya başarısız olduysa, job durumunu da güncelle
	if logUpdate.Status == "completed" || logUpdate.Status == "failed" {
		logger.Info().
			Str("process_id", logUpdate.ProcessId).
			Str("status", logUpdate.Status).
			Str("agent_id", logUpdate.AgentId).
			Msg("Process tamamlandı, job durumu güncelleniyor")

		// Coordination job'ları için özel kontrol - handleCoordinationCompletion zaten hallediyor
		if logUpdate.Metadata != nil {
			if coordinationCompletion, exists := logUpdate.Metadata["coordination_completion"]; exists && coordinationCompletion == "true" {
				logger.Debug().
					Str("process_id", logUpdate.ProcessId).
					Msg("Process coordination job olduğu için saveProcessLogs'da job update SKIP edildi (handleCoordinationCompletion halletti)")
				return nil
			}
		}

		// 🔧 FIX: Find and update job without holding lock while calling updateJobInDatabase
		s.jobMu.RLock()
		job, exists := s.jobs[logUpdate.ProcessId]

		// Create a copy if job exists
		var jobCopy *pb.Job
		if exists {
			jobCopy = &pb.Job{
				JobId:        job.JobId,
				AgentId:      job.AgentId,
				Type:         job.Type,
				Status:       job.Status,
				Result:       job.Result,
				ErrorMessage: job.ErrorMessage,
				CreatedAt:    job.CreatedAt,
				UpdatedAt:    job.UpdatedAt,
				Parameters:   make(map[string]string),
			}
			// Copy parameters
			for k, v := range job.Parameters {
				jobCopy.Parameters[k] = v
			}
		}
		s.jobMu.RUnlock()

		if exists {
			// Job durumunu güncelle
			if logUpdate.Status == "completed" {
				jobCopy.Status = pb.JobStatus_JOB_STATUS_COMPLETED
				jobCopy.Result = "Job completed successfully by agent process logger"

				// Eğer bu bir coordination job ise, özel işlem yap
				if jobCopy.Type == pb.JobType_JOB_TYPE_POSTGRES_CONVERT_TO_SLAVE {
					logger.Info().
						Str("job_id", jobCopy.JobId).
						Msg("Coordination job tamamlandı")
					jobCopy.Result = "PostgreSQL convert to slave coordination completed successfully"
				}
			} else {
				jobCopy.Status = pb.JobStatus_JOB_STATUS_FAILED
				jobCopy.ErrorMessage = "Job failed as reported by agent process logger"

				// Eğer bu bir coordination job ise, özel hata işlemi yap
				if jobCopy.Type == pb.JobType_JOB_TYPE_POSTGRES_CONVERT_TO_SLAVE {
					logger.Error().
						Str("job_id", jobCopy.JobId).
						Msg("Coordination job başarısız")
					jobCopy.ErrorMessage = "PostgreSQL convert to slave coordination failed"
				}
			}

			jobCopy.UpdatedAt = timestamppb.Now()

			// Update in memory (short lock)
			s.jobMu.Lock()
			s.jobs[logUpdate.ProcessId] = jobCopy
			s.jobMu.Unlock()

			// 🔧 FIX: Database update without holding any locks
			err = s.updateJobInDatabase(context.Background(), jobCopy)
			if err != nil {
				logger.Error().
					Err(err).
					Str("job_id", jobCopy.JobId).
					Str("process_id", logUpdate.ProcessId).
					Msg("Job durumu veritabanında güncellenirken hata")
			} else {
				if jobCopy.Type == pb.JobType_JOB_TYPE_POSTGRES_CONVERT_TO_SLAVE {
					logger.Info().
						Str("process_id", logUpdate.ProcessId).
						Str("job_status", jobCopy.Status.String()).
						Msg("Coordination job veritabanında güncellendi")
				} else {
					logger.Info().
						Str("process_id", logUpdate.ProcessId).
						Str("job_status", jobCopy.Status.String()).
						Msg("Job durumu başarıyla güncellendi")
				}
			}
		} else {
			logger.Warn().
				Str("process_id", logUpdate.ProcessId).
				Str("agent_id", logUpdate.AgentId).
				Str("status", logUpdate.Status).
				Msg("Process ID'ye karşılık gelen job bulunamadı")
		}
	}

	logger.Debug().
		Str("process_id", logUpdate.ProcessId).
		Str("agent_id", logUpdate.AgentId).
		Str("status", logUpdate.Status).
		Msg("Process logları başarıyla kaydedildi")
	return nil
}

// GetProcessStatus, belirli bir işlemin durumunu sorgular
func (s *Server) GetProcessStatus(ctx context.Context, req *pb.ProcessStatusRequest) (*pb.ProcessStatusResponse, error) {
	logger.Info().
		Str("agent_id", req.AgentId).
		Str("process_id", req.ProcessId).
		Msg("GetProcessStatus metodu çağrıldı")

	// Veritabanı bağlantısını kontrol et
	if err := s.checkDatabaseConnection(); err != nil {
		return nil, fmt.Errorf("veritabanı bağlantı hatası: %v", err)
	}

	// SQL sorgusu hazırla
	var query string
	var args []interface{}

	if req.AgentId != "" {
		// Agent ID verilmişse hem process_id hem agent_id ile sorgula
		query = `
			SELECT process_id, agent_id, process_type, status, log_messages, elapsed_time_s, metadata, created_at, updated_at
			FROM process_logs
			WHERE process_id = $1 AND agent_id = $2
		`
		args = []interface{}{req.ProcessId, req.AgentId}
	} else {
		// Agent ID verilmemişse sadece process_id ile sorgula
		query = `
			SELECT process_id, agent_id, process_type, status, log_messages, elapsed_time_s, metadata, created_at, updated_at
			FROM process_logs
			WHERE process_id = $1
		`
		args = []interface{}{req.ProcessId}
	}

	logger.Debug().
		Str("sql_query", query).
		Interface("parameters", args).
		Str("agent_id", req.AgentId).
		Str("process_id", req.ProcessId).
		Msg("Process sorgusu")

	var (
		processID       string
		agentID         string
		processType     string
		status          string
		logMessagesJSON []byte
		elapsedTimeS    float32
		metadataJSON    []byte
		createdAt       time.Time
		updatedAt       time.Time
	)

	// Sorguyu çalıştır
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&processID,
		&agentID,
		&processType,
		&status,
		&logMessagesJSON,
		&elapsedTimeS,
		&metadataJSON,
		&createdAt,
		&updatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			logger.Warn().
				Str("process_id", req.ProcessId).
				Str("agent_id", req.AgentId).
				Msg("Process bulunamadı")
			return nil, fmt.Errorf("process bulunamadı: %s", req.ProcessId)
		}
		logger.Error().
			Err(err).
			Str("process_id", req.ProcessId).
			Str("agent_id", req.AgentId).
			Msg("Process bilgileri alınırken hata")
		return nil, fmt.Errorf("process bilgileri alınırken hata: %v", err)
	}

	// Log mesajlarını çözümle
	var logMessages []string
	if err := json.Unmarshal(logMessagesJSON, &logMessages); err != nil {
		logger.Error().
			Err(err).
			Str("process_id", req.ProcessId).
			Str("agent_id", req.AgentId).
			Msg("Log mesajları ayrıştırılırken hata")
		return nil, fmt.Errorf("log mesajları ayrıştırılırken hata: %v", err)
	}

	// Metadata'yı çözümle
	var metadata map[string]string
	if err := json.Unmarshal(metadataJSON, &metadata); err != nil {
		logger.Error().
			Err(err).
			Str("process_id", req.ProcessId).
			Str("agent_id", req.AgentId).
			Msg("Metadata ayrıştırılırken hata")
		return nil, fmt.Errorf("metadata ayrıştırılırken hata: %v", err)
	}

	logger.Debug().
		Str("process_id", processID).
		Str("process_type", processType).
		Str("status", status).
		Int("log_count", len(logMessages)).
		Str("agent_id", req.AgentId).
		Msg("Process bilgileri başarıyla alındı")

	return &pb.ProcessStatusResponse{
		ProcessId:    processID,
		ProcessType:  processType,
		Status:       status,
		ElapsedTimeS: elapsedTimeS,
		LogMessages:  logMessages,
		CreatedAt:    createdAt.Format(time.RFC3339),
		UpdatedAt:    updatedAt.Format(time.RFC3339),
		Metadata:     metadata,
	}, nil
}

// handleFailoverCoordination failover koordinasyon işlemini yönetir
func (s *Server) handleFailoverCoordination(update *pb.ProcessLogUpdate, requestingAgentId string) {
	metadata := update.Metadata

	oldMasterHost := metadata["old_master_host"]
	newMasterHost := metadata["new_master_host"]
	newMasterIp := metadata["new_master_ip"] // ✅ YENİ: new_master_ip'yi metadata'dan al
	dataDirectory := metadata["data_directory"]
	replUser := metadata["replication_user"]
	replPass := metadata["replication_pass"]
	if replPass == "" {
		// Agent'ın gönderdiği alternatif anahtar adını kontrol et
		replPass = metadata["replication_password"]
	}
	action := metadata["action"]

	logger.Info().
		Str("old_master_host", oldMasterHost).
		Str("new_master_host", newMasterHost).
		Str("new_master_ip", newMasterIp).
		Str("requesting_agent_id", requestingAgentId).
		Str("action", action).
		Str("data_directory", dataDirectory).
		Str("replication_user", replUser).
		Bool("replication_password_provided", replPass != "").
		Msg("Failover koordinasyon işlemi başlatılıyor")

	if action != "convert_master_to_slave" || oldMasterHost == "" {
		logger.Error().
			Str("action", action).
			Str("old_master_host", oldMasterHost).
			Msg("Geçersiz failover koordinasyon talebi")
		return
	}

	// Replication bilgilerini kontrol et
	if replUser == "" || replPass == "" {
		logger.Error().
			Str("replication_user", replUser).
			Bool("replication_password_provided", replPass != "").
			Msg("Replication bilgileri eksik")
		return
	}

	// Güvenlik kontrolü: Talep eden agent'ın yeni master olduğunu doğrula
	expectedRequestingAgent := fmt.Sprintf("agent_%s", newMasterHost)
	if requestingAgentId != expectedRequestingAgent {
		logger.Warn().
			Str("expected_agent", expectedRequestingAgent).
			Str("requesting_agent_id", requestingAgentId).
			Str("new_master_host", newMasterHost).
			Msg("Güvenlik uyarısı: Koordinasyon talebi beklenmeyen agent'dan geldi")
		// İsteğe bağlı: Bu durumda işlemi durdurabilirsin
		// return
	} else {
		logger.Info().
			Str("requesting_agent_id", requestingAgentId).
			Str("expected_agent", expectedRequestingAgent).
			Msg("Güvenlik kontrolü başarılı: Talep eden agent doğru")
	}

	// 🔧 FIX: Lock sırasını tutarlı hale getir - ÖNCE agents, SONRA jobs
	// Eski master agent'ını bul
	oldMasterAgentId := fmt.Sprintf("agent_%s", oldMasterHost)
	logger.Info().
		Str("old_master_agent_id", oldMasterAgentId).
		Str("old_master_host", oldMasterHost).
		Msg("Eski master agent aranıyor")

	s.mu.RLock()
	oldMasterAgent, exists := s.agents[oldMasterAgentId]
	agentCount := len(s.agents)

	// Mevcut agent'ları da al
	var availableAgents []string
	for agentId := range s.agents {
		availableAgents = append(availableAgents, agentId)
	}
	// Agent bilgisini kopyala (lock'u erken bırakmak için)
	var agentStreamCopy pb.AgentService_ConnectServer
	if exists {
		agentStreamCopy = oldMasterAgent.Stream
	}
	s.mu.RUnlock()

	logger.Debug().
		Int("total_agents", agentCount).
		Str("old_master_agent_id", oldMasterAgentId).
		Bool("agent_found", exists).
		Strs("available_agents", availableAgents).
		Msg("Agent durumu")

	if !exists {
		logger.Error().
			Str("old_master_agent_id", oldMasterAgentId).
			Strs("available_agents", availableAgents).
			Int("total_agents", agentCount).
			Msg("Eski master agent bulunamadı")
		return
	}

	logger.Info().
		Str("old_master_agent_id", oldMasterAgentId).
		Msg("Eski master agent bulundu")

	// Job oluştur
	jobID := fmt.Sprintf("coord_%d", time.Now().UnixNano())
	logger.Info().
		Str("job_id", jobID).
		Str("old_master_host", oldMasterHost).
		Msg("Coordination job oluşturuluyor")

	job := &pb.Job{
		JobId:     jobID,
		Type:      pb.JobType_JOB_TYPE_POSTGRES_CONVERT_TO_SLAVE,
		Status:    pb.JobStatus_JOB_STATUS_PENDING,
		AgentId:   oldMasterAgentId,
		CreatedAt: timestamppb.Now(),
		UpdatedAt: timestamppb.Now(),
		Parameters: map[string]string{
			"old_master_host":      oldMasterHost,
			"new_master_host":      newMasterHost,
			"data_directory":       dataDirectory,
			"replication_user":     replUser,
			"replication_password": replPass,
			"requesting_agent_id":  requestingAgentId, // Koordinasyon talep eden agent
		},
	}

	// Job'ı kaydet (lock sequence: agents -> jobs)
	s.jobMu.Lock()
	s.jobs[jobID] = job
	s.jobMu.Unlock()
	logger.Info().
		Str("job_id", jobID).
		Msg("Coordination job kaydedildi")

	// 🔧 FIX: Coordination job ID'sini promotion metadata'sına ekle (job oluşturulduktan SONRA)
	go s.addCoordinationJobIdToPromotionMetadata(requestingAgentId, newMasterHost, jobID)

	// ConvertPostgresToSlave komutunu gönder
	// Format: convert_postgres_to_slave|new_master_host|new_master_ip|port|data_dir|coordination_job_id|old_master_host
	// NOT: Replication bilgileri artık agent config'inden alınacak (güvenlik)

	// ✅ FIX: newMasterIp artık metadata'dan alınıyor, fallback gerekirse uygula
	if newMasterIp == "" {
		newMasterIp = newMasterHost // Fallback: hostname'i IP olarak kullan
		logger.Warn().
			Str("new_master_host", newMasterHost).
			Msg("new_master_ip metadata'da boş, hostname fallback kullanılıyor")
	}

	command := fmt.Sprintf("convert_postgres_to_slave|%s|%s|5432|%s|%s|%s",
		newMasterHost, newMasterIp, dataDirectory, jobID, oldMasterHost)

	logger.Debug().
		Str("command", command).
		Str("old_master_agent_id", oldMasterAgentId).
		Str("job_id", jobID).
		Msg("Convert komutu hazırlandı")
	logger.Info().
		Str("old_master_agent_id", oldMasterAgentId).
		Str("old_master_host", oldMasterHost).
		Msg("Convert komutu agent'a gönderiliyor")

	// 🔧 FIX: Timeout ile Stream.Send() çağrısı
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Channel kullanarak timeout kontrollü gönderme
	sendDone := make(chan error, 1)
	go func() {
		err := agentStreamCopy.Send(&pb.ServerMessage{
			Payload: &pb.ServerMessage_Query{
				Query: &pb.Query{
					QueryId: jobID,
					Command: command,
				},
			},
		})
		sendDone <- err
	}()

	// Timeout veya başarı durumunu bekle
	select {
	case err := <-sendDone:
		if err != nil {
			logger.Error().
				Err(err).
				Str("requesting_agent_id", requestingAgentId).
				Str("old_master_agent_id", oldMasterAgentId).
				Str("job_id", jobID).
				Msg("Eski master'a convert komutu gönderilemedi")
			job.Status = pb.JobStatus_JOB_STATUS_FAILED
			job.ErrorMessage = fmt.Sprintf("Agent'a komut gönderilemedi: %v", err)
			job.UpdatedAt = timestamppb.Now()
		} else {
			logger.Info().
				Str("old_master_agent_id", oldMasterAgentId).
				Str("old_master_host", oldMasterHost).
				Str("new_master_host", newMasterHost).
				Str("job_id", jobID).
				Str("requesting_agent_id", requestingAgentId).
				Msg("Eski master'a convert komutu başarıyla gönderildi")
			job.Status = pb.JobStatus_JOB_STATUS_RUNNING
			job.UpdatedAt = timestamppb.Now()

			// 🌉 PROCESS LOG BRIDGE: Coordination başlatıldığını promotion process logs'una ekle
			go s.bridgeCoordinationLogToPromotion(requestingAgentId, newMasterHost,
				fmt.Sprintf("[%s] Coordination başlatıldı: Eski master (%s) slave'e dönüştürülüyor...",
					time.Now().Format("15:04:05"), oldMasterHost))

			// 🚀 YENİ: Diğer slave node'ları için reconfiguration komutları gönder
			go s.handleSlaveReconfiguration(metadata, newMasterHost, newMasterIp, requestingAgentId, jobID)
		}
	case <-ctx.Done():
		logger.Error().
			Str("old_master_agent_id", oldMasterAgentId).
			Str("job_id", jobID).
			Msg("Timeout: Eski master'a convert komutu gönderilemedi (10s timeout)")
		job.Status = pb.JobStatus_JOB_STATUS_FAILED
		job.ErrorMessage = "Timeout: Agent'a komut gönderilemedi (10s timeout)"
		job.UpdatedAt = timestamppb.Now()
	}

	// 🔧 FIX: Database update için de timeout context kullan
	dbCtx, dbCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dbCancel()

	if err := s.updateJobInDatabase(dbCtx, job); err != nil {
		logger.Error().
			Err(err).
			Str("job_id", jobID).
			Msg("Job veritabanında güncellenirken hata")
	} else {
		logger.Info().
			Str("job_id", jobID).
			Str("job_status", job.Status.String()).
			Msg("Coordination job durumu güncellendi")
	}
}

// handleCoordinationCompletion coordination completion işlemini yönetir
func (s *Server) handleCoordinationCompletion(update *pb.ProcessLogUpdate, reportingAgentId string) {
	logger.Info().
		Str("reporting_agent_id", reportingAgentId).
		Str("process_id", update.ProcessId).
		Msg("handleCoordinationCompletion fonksiyonu başladı")

	metadata := update.Metadata
	if metadata == nil {
		logger.Error().Msg("Metadata nil! handleCoordinationCompletion'dan çıkılıyor")
		return
	}

	coordinationJobId := metadata["coordination_job_id"]
	oldMasterHost := metadata["old_master_host"]
	newMasterHost := metadata["new_master_host"]
	action := metadata["action"]
	status := metadata["completion_status"]              // "success" or "failed"
	requestingAgentId := metadata["requesting_agent_id"] // Asıl requesting agent (promotion yapan)

	logger.Info().
		Str("coordination_job_id", coordinationJobId).
		Str("old_master_host", oldMasterHost).
		Str("new_master_host", newMasterHost).
		Str("action", action).
		Str("completion_status", status).
		Str("reporting_agent_id", reportingAgentId).
		Str("requesting_agent_id", requestingAgentId).
		Msg("Coordination completion işlemi başlatılıyor")

	if coordinationJobId == "" {
		logger.Error().Msg("Coordination job ID eksik, completion işlemi yapılamıyor")
		return
	}

	// 🔧 FIX: Find and update job without holding lock while calling other functions
	logger.Debug().
		Str("coordination_job_id", coordinationJobId).
		Msg("Job aranıyor")

	// First find the job
	s.jobMu.RLock()
	var existingJobIds []string
	var existingJobTypes []string
	for jobId, job := range s.jobs {
		existingJobIds = append(existingJobIds, jobId)
		existingJobTypes = append(existingJobTypes, job.Type.String())
	}

	logger.Debug().
		Int("total_jobs", len(s.jobs)).
		Strs("job_ids", existingJobIds).
		Strs("job_types", existingJobTypes).
		Msg("Mevcut job'lar")

	job, exists := s.jobs[coordinationJobId]
	// Create a copy to avoid pointer sharing
	var jobCopy *pb.Job
	if exists {
		jobCopy = &pb.Job{
			JobId:        job.JobId,
			AgentId:      job.AgentId,
			Type:         job.Type,
			Status:       job.Status,
			Result:       job.Result,
			ErrorMessage: job.ErrorMessage,
			CreatedAt:    job.CreatedAt,
			UpdatedAt:    job.UpdatedAt,
			Parameters:   make(map[string]string),
		}
		// Copy parameters
		for k, v := range job.Parameters {
			jobCopy.Parameters[k] = v
		}
	}
	s.jobMu.RUnlock()

	if !exists {
		logger.Error().
			Str("coordination_job_id", coordinationJobId).
			Strs("available_job_ids", existingJobIds).
			Msg("Coordination job bulunamadı")
		return
	}

	logger.Info().
		Str("coordination_job_id", coordinationJobId).
		Str("job_status", jobCopy.Status.String()).
		Msg("Coordination job bulundu")

	// Job durumunu güncelle
	logger.Info().
		Str("coordination_job_id", coordinationJobId).
		Str("incoming_status", status).
		Msg("Job status güncelleniyor")

	oldStatus := jobCopy.Status.String()
	if status == "success" || status == "completed" {
		jobCopy.Status = pb.JobStatus_JOB_STATUS_COMPLETED
		jobCopy.Result = fmt.Sprintf("Coordination completed successfully: %s converted to slave by %s", oldMasterHost, reportingAgentId)
		// FIX: Completed job'ı active coordination jobs map'inden sil
		s.jobMu.Lock()
		delete(s.jobs, coordinationJobId)
		s.jobMu.Unlock()

		// FIX: Completed job'ın processed coordination key'ini de temizle
		s.coordinationMu.Lock()
		coordinationKey := fmt.Sprintf("%s_%s_%s_convert_master_to_slave", coordinationJobId, oldMasterHost, newMasterHost)
		delete(s.processedCoordinations, coordinationKey)
		s.coordinationMu.Unlock()

		logger.Info().
			Str("coordination_job_id", coordinationJobId).
			Str("old_master_host", oldMasterHost).
			Str("reporting_agent_id", reportingAgentId).
			Msg("Coordination job başarıyla tamamlandı ve temizlendi")
	} else {
		jobCopy.Status = pb.JobStatus_JOB_STATUS_FAILED
		jobCopy.ErrorMessage = fmt.Sprintf("Coordination failed: %s could not be converted to slave", oldMasterHost)
		logger.Error().
			Str("coordination_job_id", coordinationJobId).
			Str("old_master_host", oldMasterHost).
			Msg("Coordination job başarısız")
	}

	jobCopy.UpdatedAt = timestamppb.Now()

	// Update in memory (short lock) - only if not completed
	if jobCopy.Status != pb.JobStatus_JOB_STATUS_COMPLETED {
		s.jobMu.Lock()
		s.jobs[coordinationJobId] = jobCopy
		s.jobMu.Unlock()

		logger.Info().
			Str("coordination_job_id", coordinationJobId).
			Str("old_status", oldStatus).
			Str("new_status", jobCopy.Status.String()).
			Msg("Job status değişimi")
	}

	// 🔧 FIX: Database update without holding any locks
	logger.Debug().
		Str("coordination_job_id", coordinationJobId).
		Msg("Veritabanında job güncelleniyor")
	err := s.updateJobInDatabase(context.Background(), jobCopy)
	if err != nil {
		logger.Error().
			Err(err).
			Str("coordination_job_id", coordinationJobId).
			Msg("Job veritabanında güncellenirken hata")
	} else {
		logger.Info().
			Str("coordination_job_id", coordinationJobId).
			Str("job_status", jobCopy.Status.String()).
			Msg("Coordination job veritabanında güncellendi")
	}

	// 🚀 BONUS: İlgili promotion job'unu da complete et
	// Eğer coordination başarılı olduysa, requesting agent'ın promotion job'unu da tamamla
	if status == "success" || status == "completed" {
		logger.Info().
			Str("coordination_job_id", coordinationJobId).
			Str("requesting_agent_id", requestingAgentId).
			Msg("İlgili promotion job'u aranıyor ve complete ediliyor")

		// 🔧 FIX: Bridge için doğru agent ID'sini belirle (deadlock'u önlemek için)
		bridgeAgentId := requestingAgentId
		if bridgeAgentId == "" {
			logger.Debug().
				Str("new_master_host", newMasterHost).
				Msg("requesting_agent_id bulunamadı, new_master_host ile agent aranıyor")

			// Agent'ları ayrı scope'da ara (deadlock'u önlemek için)
			foundAgentId := ""
			s.mu.RLock()
			for agentId := range s.agents {
				// Agent ID formatı genellikle "agent_hostname" şeklinde
				// agent_skadi -> skadi çıkar
				if strings.HasPrefix(agentId, "agent_") {
					hostname := strings.TrimPrefix(agentId, "agent_")
					if hostname == newMasterHost {
						foundAgentId = agentId
						break
					}
				}
			}
			s.mu.RUnlock()

			if foundAgentId != "" {
				bridgeAgentId = foundAgentId
				logger.Info().
					Str("new_master_host", newMasterHost).
					Str("bridge_agent_id", bridgeAgentId).
					Msg("new_master_host ile eşleşen agent bulundu")
			} else {
				logger.Warn().
					Str("new_master_host", newMasterHost).
					Str("fallback_agent_id", reportingAgentId).
					Msg("new_master_host ile eşleşen agent bulunamadı, fallback olarak reportingAgentId kullanılıyor")
				bridgeAgentId = reportingAgentId
			}
		}

		// 🌉 BRIDGE: Coordination completion logunu promotion process'e ekle
		displayOldMaster := oldMasterHost
		if displayOldMaster == "" {
			displayOldMaster = reportingAgentId // veya "bilinmeyen"
		}
		completionMessage := fmt.Sprintf("[%s] ✅ Coordination tamamlandı: Eski master (%s) başarıyla slave'e dönüştürüldü!",
			time.Now().Format("15:04:05"), displayOldMaster)
		go s.bridgeCoordinationLogToPromotion(bridgeAgentId, newMasterHost, completionMessage)

		// 🔧 NEW: Coordination completion'ı promotion metadata'sına ekle
		go s.updateCoordinationStatusInPromotionMetadata(bridgeAgentId, newMasterHost, coordinationJobId, "completed")

		// Final completion message
		finalMessage := fmt.Sprintf("[%s] 🎉 PostgreSQL Failover başarıyla tamamlandı! (%s -> %s)",
			time.Now().Format("15:04:05"), oldMasterHost, newMasterHost)
		go s.bridgeCoordinationLogToPromotion(bridgeAgentId, newMasterHost, finalMessage)

		// Auto-complete related promotion job
		s.completeRelatedPromotionJob(bridgeAgentId, newMasterHost)
	} else {
		// 🔧 FIX: Bridge için doğru agent ID'sini belirle (failure case - deadlock'u önlemek için)
		bridgeAgentId := requestingAgentId
		if bridgeAgentId == "" {
			logger.Debug().
				Str("new_master_host", newMasterHost).
				Msg("requesting_agent_id bulunamadı (failure case), new_master_host ile agent aranıyor")

			// Agent'ları ayrı scope'da ara (deadlock'u önlemek için)
			foundAgentId := ""
			s.mu.RLock()
			for agentId := range s.agents {
				// Agent ID formatı genellikle "agent_hostname" şeklinde
				if strings.HasPrefix(agentId, "agent_") {
					hostname := strings.TrimPrefix(agentId, "agent_")
					if hostname == newMasterHost {
						foundAgentId = agentId
						break
					}
				}
			}
			s.mu.RUnlock()

			if foundAgentId != "" {
				bridgeAgentId = foundAgentId
				logger.Info().
					Str("new_master_host", newMasterHost).
					Str("bridge_agent_id", bridgeAgentId).
					Msg("new_master_host ile eşleşen agent bulundu (failure case)")
			} else {
				logger.Warn().
					Str("new_master_host", newMasterHost).
					Str("fallback_agent_id", reportingAgentId).
					Msg("new_master_host ile eşleşen agent bulunamadı (failure case), fallback olarak reportingAgentId kullanılıyor")
				bridgeAgentId = reportingAgentId
			}
		}

		// 🌉 BRIDGE: Coordination failure logunu promotion process'e ekle
		failureMessage := fmt.Sprintf("[%s] ❌ Coordination başarısız: Eski master (%s) slave'e dönüştürülemedi!",
			time.Now().Format("15:04:05"), oldMasterHost)
		go s.bridgeCoordinationLogToPromotion(bridgeAgentId, newMasterHost, failureMessage)

		// 🔧 NEW: Coordination failure'ı promotion metadata'sına ekle
		go s.updateCoordinationStatusInPromotionMetadata(bridgeAgentId, newMasterHost, coordinationJobId, "failed")
	}

	// Coordination completion sonrasında duplicate prevention key'ini temizle
	s.cleanupCompletedCoordination(coordinationJobId, oldMasterHost, newMasterHost)

	logger.Info().
		Str("coordination_job_id", coordinationJobId).
		Str("completion_status", status).
		Msg("Coordination completion işlemi tamamlandı")
}

// handleSlaveReconfiguration diğer slave node'ları için reconfiguration komutları gönderir
func (s *Server) handleSlaveReconfiguration(metadata map[string]string, newMasterHost, newMasterIp, requestingAgentId, parentJobId string) {
	logger.Info().
		Str("new_master_host", newMasterHost).
		Str("new_master_ip", newMasterIp).
		Str("requesting_agent_id", requestingAgentId).
		Str("parent_job_id", parentJobId).
		Msg("Slave reconfiguration işlemi başlatılıyor")

	// Metadata'dan slave bilgilerini al
	slaveCount := 0
	if slaveCountStr, exists := metadata["slave_count"]; exists {
		if count, err := strconv.Atoi(slaveCountStr); err == nil {
			slaveCount = count
		}
	}

	logger.Info().
		Int("slave_count", slaveCount).
		Msg("Reconfigure edilecek slave sayısı")

	if slaveCount == 0 {
		logger.Info().Msg("Reconfigure edilecek slave node yok")
		return
	}

	// Her slave için reconfiguration komutu gönder
	for i := 0; i < slaveCount; i++ {
		slaveHostnameKey := fmt.Sprintf("slave_%d_hostname", i)
		slaveIpKey := fmt.Sprintf("slave_%d_ip", i)

		slaveHostname, hostnameExists := metadata[slaveHostnameKey]
		slaveIp, ipExists := metadata[slaveIpKey]

		if !hostnameExists || !ipExists || slaveHostname == "" {
			logger.Warn().
				Int("slave_index", i).
				Str("hostname_key", slaveHostnameKey).
				Str("ip_key", slaveIpKey).
				Bool("hostname_exists", hostnameExists).
				Bool("ip_exists", ipExists).
				Str("hostname", slaveHostname).
				Msg("Slave bilgileri eksik, atlanıyor")
			continue
		}

		logger.Info().
			Int("slave_index", i).
			Str("slave_hostname", slaveHostname).
			Str("slave_ip", slaveIp).
			Msg("Slave reconfiguration komutu gönderiliyor")

		// Slave agent'ını bul
		slaveAgentId := fmt.Sprintf("agent_%s", slaveHostname)

		s.mu.RLock()
		slaveAgent, exists := s.agents[slaveAgentId]
		var slaveStreamCopy pb.AgentService_ConnectServer
		if exists {
			slaveStreamCopy = slaveAgent.Stream
		}
		s.mu.RUnlock()

		if !exists {
			logger.Error().
				Str("slave_agent_id", slaveAgentId).
				Str("slave_hostname", slaveHostname).
				Msg("Slave agent bulunamadı, reconfiguration atlanıyor")

			// Bridge log gönder
			go s.bridgeCoordinationLogToPromotion(requestingAgentId, newMasterHost,
				fmt.Sprintf("[%s] ⚠️ Slave (%s) agent bulunamadı, reconfiguration atlandı",
					time.Now().Format("15:04:05"), slaveHostname))
			continue
		}

		// Reconfiguration komutu oluştur
		// Format: reconfigure_slave_master|new_master_host|new_master_ip|new_master_port|parent_job_id
		reconfigCommand := fmt.Sprintf("reconfigure_slave_master|%s|%s|5432|%s",
			newMasterHost, newMasterIp, parentJobId)

		logger.Debug().
			Str("reconfigure_command", reconfigCommand).
			Str("slave_agent_id", slaveAgentId).
			Str("slave_hostname", slaveHostname).
			Msg("Reconfiguration komutu hazırlandı")

		// Unique query ID oluştur
		queryID := fmt.Sprintf("reconfig_%s_%d", parentJobId, i)

		// Timeout ile komut gönder
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

		// Channel kullanarak timeout kontrollü gönderme
		sendDone := make(chan error, 1)
		go func(agentStream pb.AgentService_ConnectServer, qID, cmd string) {
			err := agentStream.Send(&pb.ServerMessage{
				Payload: &pb.ServerMessage_Query{
					Query: &pb.Query{
						QueryId: qID,
						Command: cmd,
					},
				},
			})
			sendDone <- err
		}(slaveStreamCopy, queryID, reconfigCommand)

		// Timeout veya başarı durumunu bekle
		select {
		case err := <-sendDone:
			if err != nil {
				logger.Error().
					Err(err).
					Str("slave_agent_id", slaveAgentId).
					Str("slave_hostname", slaveHostname).
					Str("query_id", queryID).
					Msg("Slave reconfiguration komutu gönderilemedi")

				// Bridge log gönder
				go s.bridgeCoordinationLogToPromotion(requestingAgentId, newMasterHost,
					fmt.Sprintf("[%s] ❌ Slave (%s) reconfiguration komutu gönderilemedi: %v",
						time.Now().Format("15:04:05"), slaveHostname, err))
			} else {
				logger.Info().
					Str("slave_agent_id", slaveAgentId).
					Str("slave_hostname", slaveHostname).
					Str("query_id", queryID).
					Str("new_master_host", newMasterHost).
					Str("new_master_ip", newMasterIp).
					Msg("Slave reconfiguration komutu başarıyla gönderildi")

				// Bridge log gönder
				go s.bridgeCoordinationLogToPromotion(requestingAgentId, newMasterHost,
					fmt.Sprintf("[%s] 🔄 Slave (%s) reconfiguration başlatıldı: %s -> %s",
						time.Now().Format("15:04:05"), slaveHostname, slaveHostname, newMasterHost))
			}
		case <-ctx.Done():
			logger.Error().
				Str("slave_agent_id", slaveAgentId).
				Str("slave_hostname", slaveHostname).
				Str("query_id", queryID).
				Msg("Timeout: Slave reconfiguration komutu gönderilemedi (10s timeout)")

			// Bridge log gönder
			go s.bridgeCoordinationLogToPromotion(requestingAgentId, newMasterHost,
				fmt.Sprintf("[%s] ⏰ Slave (%s) reconfiguration timeout (10s)",
					time.Now().Format("15:04:05"), slaveHostname))
		}

		cancel()
	}

	logger.Info().
		Int("slave_count", slaveCount).
		Str("new_master_host", newMasterHost).
		Msg("Slave reconfiguration komutları gönderildi")
}

// completeRelatedPromotionJob ilgili promotion job'unu complete eder
func (s *Server) completeRelatedPromotionJob(requestingAgentId, newMasterHost string) {
	logger.Info().
		Str("requesting_agent_id", requestingAgentId).
		Str("new_master_host", newMasterHost).
		Msg("Agent'ının promotion job'u aranıyor")

	// 🔧 FIX: Find the job first without holding the lock for too long
	var promotionJob *pb.Job
	var promotionJobId string

	s.jobMu.RLock()
	// Requesting agent'a ait RUNNING promotion job'larını ara
	for jobId, job := range s.jobs {
		// Promotion job kriterleri:
		// 1. Agent ID eşleşmeli
		// 2. Type postgresql_promotion olmalı
		// 3. Status RUNNING olmalı
		// 4. Node hostname new master ile eşleşmeli (opsiyonel kontrol)
		if job.AgentId == requestingAgentId &&
			job.Type == pb.JobType_JOB_TYPE_POSTGRES_PROMOTE_MASTER &&
			job.Status == pb.JobStatus_JOB_STATUS_RUNNING {

			// Node hostname kontrolü (eğer varsa)
			if nodeHostname, exists := job.Parameters["node_hostname"]; exists {
				if nodeHostname == newMasterHost {
					// Job'un kopyasını oluştur (pointer paylaşımından kaçınmak için)
					promotionJob = &pb.Job{
						JobId:        job.JobId,
						AgentId:      job.AgentId,
						Type:         job.Type,
						Status:       job.Status,
						Result:       job.Result,
						ErrorMessage: job.ErrorMessage,
						CreatedAt:    job.CreatedAt,
						UpdatedAt:    job.UpdatedAt,
						Parameters:   make(map[string]string),
					}
					// Parameters'ı kopyala
					for k, v := range job.Parameters {
						promotionJob.Parameters[k] = v
					}
					promotionJobId = jobId
					logger.Info().
						Str("job_id", jobId).
						Str("node_hostname", nodeHostname).
						Str("new_master_host", newMasterHost).
						Msg("Eşleşen promotion job bulundu")
					break
				} else {
					logger.Debug().
						Str("job_id", jobId).
						Str("node_hostname", nodeHostname).
						Str("new_master_host", newMasterHost).
						Msg("Promotion job bulundu ama node hostname eşleşmiyor")
				}
			} else {
				// Node hostname yoksa, agent ve type eşleşen ilk job'u al
				promotionJob = &pb.Job{
					JobId:        job.JobId,
					AgentId:      job.AgentId,
					Type:         job.Type,
					Status:       job.Status,
					Result:       job.Result,
					ErrorMessage: job.ErrorMessage,
					CreatedAt:    job.CreatedAt,
					UpdatedAt:    job.UpdatedAt,
					Parameters:   make(map[string]string),
				}
				// Parameters'ı kopyala
				for k, v := range job.Parameters {
					promotionJob.Parameters[k] = v
				}
				promotionJobId = jobId
				logger.Info().
					Str("job_id", jobId).
					Msg("Promotion job bulundu (node hostname bilgisi yok)")
				break
			}
		}
	}
	s.jobMu.RUnlock()

	if promotionJob == nil {
		logger.Warn().
			Str("requesting_agent_id", requestingAgentId).
			Str("new_master_host", newMasterHost).
			Msg("Agent için RUNNING promotion job bulunamadı")

		// Debug: Mevcut job'ları listele (ayrı lock ile)
		s.jobMu.RLock()
		var agentJobs []string
		for jobId, job := range s.jobs {
			if job.AgentId == requestingAgentId {
				agentJobs = append(agentJobs, fmt.Sprintf("%s:%s:%s", jobId, job.Type.String(), job.Status.String()))
			}
		}
		s.jobMu.RUnlock()

		logger.Debug().
			Str("requesting_agent_id", requestingAgentId).
			Strs("agent_jobs", agentJobs).
			Msg("Mevcut job'lar")
		return
	}

	// 🔧 FIX: Update the job without holding lock for too long
	oldStatus := promotionJob.Status.String()
	promotionJob.Status = pb.JobStatus_JOB_STATUS_COMPLETED
	promotionJob.Result = fmt.Sprintf("PostgreSQL promotion completed successfully. Coordination also completed for old master conversion.")
	promotionJob.UpdatedAt = timestamppb.Now()

	// Now update in memory (short lock)
	s.jobMu.Lock()
	s.jobs[promotionJobId] = promotionJob
	s.jobMu.Unlock()

	logger.Info().
		Str("promotion_job_id", promotionJobId).
		Str("old_status", oldStatus).
		Str("new_status", promotionJob.Status.String()).
		Msg("Promotion job status değişimi")

	// 🔧 FIX: Database and process logs updates without holding any locks
	// Veritabanında da güncelle (no locks held)
	err := s.updateJobInDatabase(context.Background(), promotionJob)
	if err != nil {
		logger.Error().
			Err(err).
			Str("promotion_job_id", promotionJobId).
			Msg("Promotion job veritabanında güncellenirken hata")
	} else {
		logger.Info().
			Str("promotion_job_id", promotionJobId).
			Str("job_status", promotionJob.Status.String()).
			Msg("Promotion job veritabanında güncellendi")
	}

	// 🔧 FIX: Mevcut metadata'yı al ve completion bilgilerini ekle (override etme!)
	completionMetadata := make(map[string]string)

	// Veritabanından mevcut metadata'yı al
	var completionMetadataJSON []byte
	completionDbErr := s.db.QueryRow(`
		SELECT metadata FROM process_logs 
		WHERE process_id = $1 AND agent_id = $2
	`, promotionJobId, requestingAgentId).Scan(&completionMetadataJSON)

	if completionDbErr == nil && len(completionMetadataJSON) > 0 {
		// Mevcut metadata'yı parse et
		if parseErr := json.Unmarshal(completionMetadataJSON, &completionMetadata); parseErr != nil {
			logger.Warn().
				Err(parseErr).
				Str("promotion_job_id", promotionJobId).
				Msg("COMPLETION: Mevcut metadata parse edilemedi, yeni metadata oluşturuluyor")
			completionMetadata = make(map[string]string)
		}
	}

	// Completion bilgilerini mevcut metadata'ya ekle (override etmeden)
	completionMetadata["auto_completed"] = "true"
	completionMetadata["completion_source"] = "coordination_system"
	completionMetadata["completed_at"] = time.Now().Format(time.RFC3339)

	// 🔧 FIX: Process logs tablosunu da "completed" olarak güncelle (no locks held)
	completionLogUpdate := &pb.ProcessLogUpdate{
		AgentId:      requestingAgentId,
		ProcessId:    promotionJobId,
		ProcessType:  "postgresql_promotion",
		Status:       "completed", // Running'den completed'e güncelle
		LogMessages:  []string{fmt.Sprintf("[%s] 🎉 PostgreSQL promotion başarıyla tamamlandı! (Auto-completed by coordination system)", time.Now().Format("15:04:05"))},
		ElapsedTimeS: 0,
		UpdatedAt:    time.Now().Format(time.RFC3339),
		Metadata:     completionMetadata, // Merged metadata kullan
	}

	err = s.saveProcessLogs(context.Background(), completionLogUpdate)
	if err != nil {
		logger.Error().
			Err(err).
			Str("promotion_job_id", promotionJobId).
			Msg("Promotion process logs güncellenirken hata")
	} else {
		logger.Info().
			Str("promotion_job_id", promotionJobId).
			Msg("Promotion process logs da 'completed' olarak güncellendi")
	}

	logger.Info().
		Str("promotion_job_id", promotionJobId).
		Str("requesting_agent_id", requestingAgentId).
		Msg("İlgili promotion job başarıyla complete edildi")
}

// addCoordinationJobIdToPromotionMetadata coordination job ID'sini promotion process metadata'sına ekler
func (s *Server) addCoordinationJobIdToPromotionMetadata(requestingAgentId, newMasterHost, coordinationJobId string) {
	logger.Debug().
		Str("requesting_agent_id", requestingAgentId).
		Str("new_master_host", newMasterHost).
		Str("coordination_job_id", coordinationJobId).
		Msg("Coordination job ID promotion metadata'sına ekleniyor")

	// 🔧 FIX: Find promotion process ID without holding lock
	var promotionProcessId string

	s.jobMu.RLock()
	var availableJobs []string
	for jobId, job := range s.jobs {
		availableJobs = append(availableJobs, fmt.Sprintf("%s:%s:%s", jobId, job.Type.String(), job.AgentId))

		if job.AgentId == requestingAgentId &&
			job.Type == pb.JobType_JOB_TYPE_POSTGRES_PROMOTE_MASTER &&
			(job.Status == pb.JobStatus_JOB_STATUS_RUNNING || job.Status == pb.JobStatus_JOB_STATUS_COMPLETED) {

			// Node hostname kontrolü (opsiyonel)
			if nodeHostname, exists := job.Parameters["node_hostname"]; exists {
				if nodeHostname == newMasterHost {
					promotionProcessId = jobId
					logger.Info().
						Str("job_id", jobId).
						Str("node_hostname", nodeHostname).
						Str("new_master_host", newMasterHost).
						Msg("✅ Promotion process ID bulundu (hostname match)")
					break
				}
			} else {
				// Node hostname yoksa ilk eşleşen job'u al
				promotionProcessId = jobId
				logger.Info().
					Str("job_id", jobId).
					Msg("✅ Promotion process ID bulundu (hostname yok)")
				break
			}
		}
	}
	s.jobMu.RUnlock()

	logger.Debug().
		Str("requesting_agent_id", requestingAgentId).
		Str("new_master_host", newMasterHost).
		Strs("available_jobs", availableJobs).
		Str("found_promotion_process_id", promotionProcessId).
		Msg("Job arama sonucu")

	if promotionProcessId == "" {
		logger.Warn().
			Str("requesting_agent_id", requestingAgentId).
			Str("new_master_host", newMasterHost).
			Msg("Promotion process bulunamadı, coordination job ID eklenemedi")
		return
	}

	// Mevcut metadata'yı al ve coordination job ID'sini ekle
	var existingMetadata map[string]string
	var existingMetadataJSON []byte

	err := s.db.QueryRow(`
		SELECT metadata FROM process_logs 
		WHERE process_id = $1 AND agent_id = $2
	`, promotionProcessId, requestingAgentId).Scan(&existingMetadataJSON)

	if err != nil {
		logger.Error().
			Err(err).
			Str("promotion_process_id", promotionProcessId).
			Msg("Promotion process metadata alınamadı")
		return
	}

	// Mevcut metadata'yı parse et
	if err := json.Unmarshal(existingMetadataJSON, &existingMetadata); err != nil {
		logger.Error().
			Err(err).
			Str("promotion_process_id", promotionProcessId).
			Msg("Promotion process metadata parse edilemedi")
		return
	}

	// Coordination job ID'sini ekle
	existingMetadata["coordination_job_id"] = coordinationJobId
	existingMetadata["coordination_status"] = "started"
	existingMetadata["coordination_added_at"] = time.Now().Format(time.RFC3339)

	logger.Info().
		Str("promotion_process_id", promotionProcessId).
		Str("coordination_job_id", coordinationJobId).
		Interface("updated_metadata", existingMetadata).
		Msg("Metadata güncelleniyor")

	// Güncellenmiş metadata'yı JSON'a çevir
	updatedMetadataJSON, err := json.Marshal(existingMetadata)
	if err != nil {
		logger.Error().
			Err(err).
			Str("promotion_process_id", promotionProcessId).
			Msg("Güncellenmiş metadata JSON'a çevrilemedi")
		return
	}

	logger.Debug().
		Str("promotion_process_id", promotionProcessId).
		Str("requesting_agent_id", requestingAgentId).
		Str("updated_metadata_json", string(updatedMetadataJSON)).
		Msg("Database update SQL çalıştırılıyor")

	// Veritabanında güncelle
	result, err := s.db.Exec(`
		UPDATE process_logs SET 
			metadata = $1,
			updated_at = $2
		WHERE process_id = $3 AND agent_id = $4
	`, updatedMetadataJSON, time.Now(), promotionProcessId, requestingAgentId)

	if err != nil {
		logger.Error().
			Err(err).
			Str("promotion_process_id", promotionProcessId).
			Str("coordination_job_id", coordinationJobId).
			Msg("❌ Promotion process metadata güncellenemedi")
	} else {
		// Etkilenen satır sayısını kontrol et
		rowsAffected, _ := result.RowsAffected()
		logger.Info().
			Str("promotion_process_id", promotionProcessId).
			Str("coordination_job_id", coordinationJobId).
			Int64("rows_affected", rowsAffected).
			Msg("✅ Coordination job ID başarıyla promotion metadata'sına eklendi")
	}
}

// updateCoordinationStatusInPromotionMetadata coordination status'unu promotion metadata'sında günceller
func (s *Server) updateCoordinationStatusInPromotionMetadata(requestingAgentId, newMasterHost, coordinationJobId, status string) {
	logger.Debug().
		Str("requesting_agent_id", requestingAgentId).
		Str("new_master_host", newMasterHost).
		Str("coordination_job_id", coordinationJobId).
		Str("status", status).
		Msg("Coordination status promotion metadata'sında güncelleniyor")

	// 🔧 FIX: Find promotion process ID without holding lock
	var promotionProcessId string

	s.jobMu.RLock()
	for jobId, job := range s.jobs {
		if job.AgentId == requestingAgentId &&
			job.Type == pb.JobType_JOB_TYPE_POSTGRES_PROMOTE_MASTER {

			// Node hostname kontrolü (opsiyonel)
			if nodeHostname, exists := job.Parameters["node_hostname"]; exists {
				if nodeHostname == newMasterHost {
					promotionProcessId = jobId
					break
				}
			} else {
				// Node hostname yoksa ilk eşleşen job'u al
				promotionProcessId = jobId
				break
			}
		}
	}
	s.jobMu.RUnlock()

	if promotionProcessId == "" {
		logger.Warn().
			Str("requesting_agent_id", requestingAgentId).
			Str("new_master_host", newMasterHost).
			Msg("Promotion process bulunamadı, coordination status güncellenemedi")
		return
	}

	// Mevcut metadata'yı al ve coordination status'unu güncelle
	var existingMetadata map[string]string
	var existingMetadataJSON []byte

	err := s.db.QueryRow(`
		SELECT metadata FROM process_logs 
		WHERE process_id = $1 AND agent_id = $2
	`, promotionProcessId, requestingAgentId).Scan(&existingMetadataJSON)

	if err != nil {
		logger.Error().
			Err(err).
			Str("promotion_process_id", promotionProcessId).
			Msg("Promotion process metadata alınamadı")
		return
	}

	// Mevcut metadata'yı parse et
	if err := json.Unmarshal(existingMetadataJSON, &existingMetadata); err != nil {
		logger.Error().
			Err(err).
			Str("promotion_process_id", promotionProcessId).
			Msg("Promotion process metadata parse edilemedi")
		return
	}

	// Coordination status'unu güncelle
	existingMetadata["coordination_status"] = status
	if status == "completed" {
		existingMetadata["coordination_completed_at"] = time.Now().Format(time.RFC3339)
	} else if status == "failed" {
		existingMetadata["coordination_failed_at"] = time.Now().Format(time.RFC3339)
	}

	// Güncellenmiş metadata'yı JSON'a çevir
	updatedMetadataJSON, err := json.Marshal(existingMetadata)
	if err != nil {
		logger.Error().
			Err(err).
			Str("promotion_process_id", promotionProcessId).
			Msg("Güncellenmiş metadata JSON'a çevrilemedi")
		return
	}

	// Veritabanında güncelle
	_, err = s.db.Exec(`
		UPDATE process_logs SET 
			metadata = $1,
			updated_at = $2
		WHERE process_id = $3 AND agent_id = $4
	`, updatedMetadataJSON, time.Now(), promotionProcessId, requestingAgentId)

	if err != nil {
		logger.Error().
			Err(err).
			Str("promotion_process_id", promotionProcessId).
			Str("coordination_job_id", coordinationJobId).
			Str("status", status).
			Msg("Promotion process coordination status güncellenemedi")
	} else {
		logger.Info().
			Str("promotion_process_id", promotionProcessId).
			Str("coordination_job_id", coordinationJobId).
			Str("status", status).
			Msg("Coordination status başarıyla promotion metadata'sında güncellendi")
	}
}

// bridgeCoordinationLogToPromotion coordination loglarını promotion process logs'una bridge eder
func (s *Server) bridgeCoordinationLogToPromotion(requestingAgentId, newMasterHost, logMessage string) {
	logger.Debug().
		Str("requesting_agent_id", requestingAgentId).
		Str("new_master_host", newMasterHost).
		Str("log_message", logMessage).
		Msg("BRIDGE: Agent'ının promotion process'ine log ekleniyor")

	// 🔧 FIX: Find promotion process ID without holding lock while calling saveProcessLogs
	var promotionProcessId string

	s.jobMu.RLock()
	for jobId, job := range s.jobs {
		if job.AgentId == requestingAgentId &&
			job.Type == pb.JobType_JOB_TYPE_POSTGRES_PROMOTE_MASTER &&
			(job.Status == pb.JobStatus_JOB_STATUS_RUNNING || job.Status == pb.JobStatus_JOB_STATUS_COMPLETED) {

			// Node hostname kontrolü (opsiyonel)
			if nodeHostname, exists := job.Parameters["node_hostname"]; exists {
				if nodeHostname == newMasterHost {
					promotionProcessId = jobId
					logger.Debug().
						Str("job_id", jobId).
						Str("node_hostname", nodeHostname).
						Str("new_master_host", newMasterHost).
						Msg("BRIDGE: Promotion process ID bulundu")
					break
				}
			} else {
				// Node hostname yoksa ilk eşleşen job'u al
				promotionProcessId = jobId
				logger.Debug().
					Str("job_id", jobId).
					Msg("BRIDGE: Promotion process ID bulundu (hostname yok)")
				break
			}
		}
	}
	s.jobMu.RUnlock()

	if promotionProcessId == "" {
		logger.Warn().
			Str("requesting_agent_id", requestingAgentId).
			Str("new_master_host", newMasterHost).
			Msg("BRIDGE: Agent'ının promotion process'i bulunamadı")
		return
	}

	// 🔧 FIX: Mevcut metadata'yı al ve bridge bilgilerini ekle (override etme!)
	existingMetadata := make(map[string]string)

	// Veritabanından mevcut metadata'yı al
	var metadataJSON []byte
	dbErr := s.db.QueryRow(`
		SELECT metadata FROM process_logs 
		WHERE process_id = $1 AND agent_id = $2
	`, promotionProcessId, requestingAgentId).Scan(&metadataJSON)

	if dbErr == nil && len(metadataJSON) > 0 {
		// Mevcut metadata'yı parse et
		if parseErr := json.Unmarshal(metadataJSON, &existingMetadata); parseErr != nil {
			logger.Warn().
				Err(parseErr).
				Str("promotion_process_id", promotionProcessId).
				Msg("BRIDGE: Mevcut metadata parse edilemedi, yeni metadata oluşturuluyor")
			existingMetadata = make(map[string]string)
		}
	}

	// Bridge bilgilerini mevcut metadata'ya ekle (override etmeden)
	existingMetadata["bridge_source"] = "coordination"
	existingMetadata["bridge_type"] = "coordination_update"
	existingMetadata["bridge_last_update"] = time.Now().Format(time.RFC3339)

	// 🔧 FIX: Process log update oluştur ve kaydet (hiç lock tutmadan)
	logUpdate := &pb.ProcessLogUpdate{
		AgentId:      requestingAgentId,
		ProcessId:    promotionProcessId,
		ProcessType:  "postgresql_promotion",
		Status:       "running", // Hala devam ediyor
		LogMessages:  []string{logMessage},
		ElapsedTimeS: 0,
		UpdatedAt:    time.Now().Format(time.RFC3339),
		Metadata:     existingMetadata, // Merged metadata kullan
	}

	// Process logs'a kaydet (no locks held - prevents deadlock)
	err := s.saveProcessLogs(context.Background(), logUpdate)
	if err != nil {
		logger.Error().
			Err(err).
			Str("promotion_process_id", promotionProcessId).
			Str("requesting_agent_id", requestingAgentId).
			Msg("BRIDGE: Log kaydedilirken hata")
	} else {
		logger.Debug().
			Str("promotion_process_id", promotionProcessId).
			Str("requesting_agent_id", requestingAgentId).
			Msg("BRIDGE: Coordination log başarıyla promotion process'e eklendi")
	}
}

// cleanupOldCoordinations, eski coordination kayıtlarını temizler (memory leak prevention)
func (s *Server) cleanupOldCoordinations() {
	s.coordinationMu.Lock()
	defer s.coordinationMu.Unlock()

	cutoff := time.Now().Add(-5 * time.Minute) // 🔧 FIX: 5 dakikadan eski olanları temizle (daha agresif)
	keysToDelete := make([]string, 0)

	for key, timestamp := range s.processedCoordinations {
		if timestamp.Before(cutoff) {
			keysToDelete = append(keysToDelete, key)
		}
	}

	for _, key := range keysToDelete {
		delete(s.processedCoordinations, key)
	}

	if len(keysToDelete) > 0 {
		logger.Info().
			Int("cleaned_records", len(keysToDelete)).
			Dur("older_than", 5*time.Minute).
			Msg("Cleaned up old coordination records (aggressive cleanup)")
	}
}

// cleanupCompletedCoordination, tamamlanan coordination'ın duplicate prevention key'ini temizler
func (s *Server) cleanupCompletedCoordination(coordinationJobId, oldMasterHost, newMasterHost string) {
	s.coordinationMu.Lock()
	defer s.coordinationMu.Unlock()

	// Coordination key formatını oluştur (aynı format kullanılmalı)
	// Format: {process_id}_{old_master_host}_{new_master_host}_{action}
	coordinationKey := fmt.Sprintf("%s_%s_%s_convert_master_to_slave",
		coordinationJobId,
		oldMasterHost,
		newMasterHost)

	// Eğer bu key varsa, temizle
	if _, exists := s.processedCoordinations[coordinationKey]; exists {
		delete(s.processedCoordinations, coordinationKey)
		logger.Info().
			Str("coordination_key", coordinationKey).
			Str("coordination_job_id", coordinationJobId).
			Msg("Completed coordination key cleaned up from duplicate prevention")
	}

	// Fallback: Job ID'si prefix olan tüm key'leri temizle (güvenlik önlemi)
	keysToDelete := make([]string, 0)
	for key := range s.processedCoordinations {
		if strings.HasPrefix(key, coordinationJobId+"_") {
			keysToDelete = append(keysToDelete, key)
		}
	}

	for _, key := range keysToDelete {
		delete(s.processedCoordinations, key)
		logger.Debug().
			Str("fallback_key", key).
			Str("coordination_job_id", coordinationJobId).
			Msg("Fallback cleanup: removed coordination key with job ID prefix")
	}
}

// GetCoordinationStatus, mevcut coordination state'ini döndürür
func (s *Server) GetCoordinationStatus() (map[string]time.Time, map[string]*pb.Job) {
	// Coordination keys'i ayrı lock ile al
	coordinationKeys := make(map[string]time.Time)
	s.coordinationMu.RLock()
	for k, v := range s.processedCoordinations {
		coordinationKeys[k] = v
	}
	s.coordinationMu.RUnlock()

	// Active coordination jobs'ları ayrı lock ile al (deadlock'u önlemek için)
	activeJobs := make(map[string]*pb.Job)
	s.jobMu.RLock()
	for jobId, job := range s.jobs {
		// Sadece CONVERT_TO_SLAVE tipindeki ve COMPLETED olmayan job'ları al
		if job.Type == pb.JobType_JOB_TYPE_POSTGRES_CONVERT_TO_SLAVE &&
			job.Status != pb.JobStatus_JOB_STATUS_COMPLETED {
			// Job'un kopyasını oluştur (pointer referansından kaçınmak için)
			jobCopy := &pb.Job{
				JobId:        job.JobId,
				AgentId:      job.AgentId,
				Type:         job.Type,
				Status:       job.Status,
				Result:       job.Result,
				ErrorMessage: job.ErrorMessage,
				CreatedAt:    job.CreatedAt,
				UpdatedAt:    job.UpdatedAt,
				Parameters:   make(map[string]string),
			}
			// Parameters'ı kopyala
			for k, v := range job.Parameters {
				jobCopy.Parameters[k] = v
			}
			activeJobs[jobId] = jobCopy
		}
	}
	s.jobMu.RUnlock()

	return coordinationKeys, activeJobs
}

// CleanupAllCoordination, tüm coordination state'ini temizler
func (s *Server) CleanupAllCoordination() int {
	s.coordinationMu.Lock()
	defer s.coordinationMu.Unlock()

	count := len(s.processedCoordinations)
	s.processedCoordinations = make(map[string]time.Time)

	logger.Info().
		Int("cleaned_count", count).
		Msg("All coordination state cleaned up")

	return count
}

// CleanupOldCoordination, eski coordination kayıtlarını temizler
func (s *Server) CleanupOldCoordination() int {
	s.coordinationMu.Lock()
	defer s.coordinationMu.Unlock()

	cutoff := time.Now().Add(-5 * time.Minute) // 🔧 FIX: Tutarlı 5 dakika timeout
	keysToDelete := make([]string, 0)

	for key, timestamp := range s.processedCoordinations {
		if timestamp.Before(cutoff) {
			keysToDelete = append(keysToDelete, key)
		}
	}

	for _, key := range keysToDelete {
		delete(s.processedCoordinations, key)
	}

	if len(keysToDelete) > 0 {
		logger.Info().
			Int("cleaned_count", len(keysToDelete)).
			Dur("older_than", 5*time.Minute).
			Msg("Old coordination records cleaned up (API request)")
	}

	return len(keysToDelete)
}

// CleanupCoordinationKey, belirli bir coordination key'ini temizler
func (s *Server) CleanupCoordinationKey(key string) bool {
	s.coordinationMu.Lock()
	defer s.coordinationMu.Unlock()

	if _, exists := s.processedCoordinations[key]; exists {
		delete(s.processedCoordinations, key)
		logger.Info().
			Str("coordination_key", key).
			Msg("Specific coordination key cleaned up")
		return true
	}

	return false
}

// 🔧 NEW: CleanupStuckCoordinationJobs cleans up coordination jobs that are stuck
func (s *Server) CleanupStuckCoordinationJobs() int {
	s.jobMu.Lock()
	defer s.jobMu.Unlock()

	cutoff := time.Now().Add(-10 * time.Minute) // Jobs stuck for more than 10 minutes
	stuckJobsCount := 0

	for jobId, job := range s.jobs {
		if job.Type == pb.JobType_JOB_TYPE_POSTGRES_CONVERT_TO_SLAVE {
			// Check if job is stuck in PENDING or RUNNING state for too long
			if (job.Status == pb.JobStatus_JOB_STATUS_PENDING || job.Status == pb.JobStatus_JOB_STATUS_RUNNING) &&
				job.CreatedAt.AsTime().Before(cutoff) {

				logger.Warn().
					Str("job_id", jobId).
					Str("job_status", job.Status.String()).
					Dur("age", time.Since(job.CreatedAt.AsTime())).
					Msg("Cleaning up stuck coordination job")

				// Mark job as failed
				job.Status = pb.JobStatus_JOB_STATUS_FAILED
				job.ErrorMessage = "Job timed out and was automatically cleaned up"
				job.UpdatedAt = timestamppb.Now()

				s.jobs[jobId] = job
				stuckJobsCount++

				// Also update in database
				go func(j *pb.Job) {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					if err := s.updateJobInDatabase(ctx, j); err != nil {
						logger.Error().
							Err(err).
							Str("job_id", j.JobId).
							Msg("Failed to update stuck job in database")
					}
				}(job)
			}
		}
	}

	if stuckJobsCount > 0 {
		logger.Info().
			Int("cleaned_jobs", stuckJobsCount).
			Msg("Cleaned up stuck coordination jobs")
	}

	return stuckJobsCount
}

// 🚨 EMERGENCY: CleanupEmergencyDeadlock cleans up any potential deadlock situation
func (s *Server) CleanupEmergencyDeadlock() map[string]int {
	logger.Warn().Msg("🚨 EMERGENCY DEADLOCK CLEANUP BAŞLADI")

	result := make(map[string]int)

	// 1. Clean all coordination state
	result["coordination_keys"] = s.CleanupAllCoordination()

	// 2. Clean stuck coordination jobs
	result["stuck_jobs"] = s.CleanupStuckCoordinationJobs()

	// 3. Clean ALL promotion jobs that are stuck (more aggressive)
	s.jobMu.Lock()
	promotionStuckCount := 0
	cutoff := time.Now().Add(-5 * time.Minute) // More aggressive - 5 minutes

	for jobId, job := range s.jobs {
		if job.Type == pb.JobType_JOB_TYPE_POSTGRES_PROMOTE_MASTER {
			if (job.Status == pb.JobStatus_JOB_STATUS_PENDING || job.Status == pb.JobStatus_JOB_STATUS_RUNNING) &&
				job.CreatedAt.AsTime().Before(cutoff) {

				logger.Warn().
					Str("job_id", jobId).
					Str("job_type", job.Type.String()).
					Str("job_status", job.Status.String()).
					Dur("age", time.Since(job.CreatedAt.AsTime())).
					Msg("🚨 EMERGENCY: Cleaning up stuck promotion job")

				job.Status = pb.JobStatus_JOB_STATUS_FAILED
				job.ErrorMessage = "Emergency cleanup: Job was stuck and cleaned up"
				job.UpdatedAt = timestamppb.Now()
				s.jobs[jobId] = job
				promotionStuckCount++
			}
		}
	}
	s.jobMu.Unlock()

	result["stuck_promotions"] = promotionStuckCount

	logger.Warn().
		Int("coordination_keys", result["coordination_keys"]).
		Int("stuck_jobs", result["stuck_jobs"]).
		Int("stuck_promotions", result["stuck_promotions"]).
		Msg("🚨 EMERGENCY CLEANUP TAMAMLANDI")

	return result
}

// 🔧 NEW: ForceCompleteProcess manually completes a stuck process
func (s *Server) ForceCompleteProcess(processId string) bool {
	logger.Warn().
		Str("process_id", processId).
		Msg("🔧 FORCE COMPLETING STUCK PROCESS")

		// Check if process exists in database first
	var currentStatus string
	var agentId string
	var processType string

	err := s.db.QueryRow(`
		SELECT status, agent_id, process_type
		FROM process_logs 
		WHERE process_id = $1
	`, processId).Scan(&currentStatus, &agentId, &processType)

	if err != nil {
		logger.Error().
			Err(err).
			Str("process_id", processId).
			Msg("Process bulunamadı")
		return false
	}
	logger.Info().
		Str("process_id", processId).
		Str("current_status", currentStatus).
		Str("agent_id", agentId).
		Str("process_type", processType).
		Msg("Process bulundu, zorla tamamlanıyor")

	if currentStatus == "completed" {
		logger.Info().
			Str("process_id", processId).
			Msg("Process zaten completed durumunda")
		return true
	}

	// Force update process logs to completed
	completionTime := time.Now()

	// Get existing logs first
	var existingLogsJSON []byte
	err = s.db.QueryRow(`
		SELECT log_messages FROM process_logs WHERE process_id = $1
	`, processId).Scan(&existingLogsJSON)

	if err != nil {
		logger.Error().
			Err(err).
			Str("process_id", processId).
			Msg("Existing logs alınamadı")
		return false
	}

	// Parse existing logs
	var existingLogs []string
	if err := json.Unmarshal(existingLogsJSON, &existingLogs); err != nil {
		logger.Error().
			Err(err).
			Str("process_id", processId).
			Msg("Existing logs parse edilemedi")
		return false
	}

	// Add completion message
	completionMessage := fmt.Sprintf("[%s] 🔧 FORCE COMPLETED: Process manually completed via API",
		completionTime.Format("15:04:05"))
	existingLogs = append(existingLogs, completionMessage)

	// Convert back to JSON
	updatedLogsJSON, err := json.Marshal(existingLogs)
	if err != nil {
		logger.Error().
			Err(err).
			Str("process_id", processId).
			Msg("Updated logs JSON'a çevrilemedi")
		return false
	}

	// Update metadata to include force completion info
	metadata := map[string]string{
		"force_completed":   "true",
		"completion_source": "manual_api",
		"completion_time":   completionTime.Format(time.RFC3339),
	}
	metadataJSON, _ := json.Marshal(metadata)

	// Force update in database
	_, err = s.db.Exec(`
		UPDATE process_logs SET 
			status = 'completed',
			log_messages = $1,
			metadata = $2,
			updated_at = $3
		WHERE process_id = $4
	`, updatedLogsJSON, metadataJSON, completionTime, processId)

	if err != nil {
		logger.Error().
			Err(err).
			Str("process_id", processId).
			Msg("Process force completion veritabanında güncellenemedi")
		return false
	}

	// Also update in-memory job if it exists
	s.jobMu.Lock()
	if job, jobExists := s.jobs[processId]; jobExists {
		job.Status = pb.JobStatus_JOB_STATUS_COMPLETED
		job.Result = "Process force completed via API"
		job.UpdatedAt = timestamppb.Now()
		s.jobs[processId] = job
	}
	s.jobMu.Unlock()

	logger.Warn().
		Str("process_id", processId).
		Str("agent_id", agentId).
		Str("process_type", processType).
		Msg("🔧 PROCESS FORCE COMPLETED SUCCESSFULLY")

	return true
}

// RollbackPostgresFailover PostgreSQL failover işlemini geri alır
func (s *Server) RollbackPostgresFailover(ctx context.Context, req *pb.PostgresRollbackRequest) (*pb.PostgresRollbackResponse, error) {
	logger.Info().
		Str("agent_id", req.AgentId).
		Str("job_id", req.JobId).
		Str("reason", req.Reason).
		Msg("PostgreSQL rollback işlemi başlatılıyor")

	// Agent'ı bul
	s.mu.RLock()
	agent, exists := s.agents[req.AgentId]
	s.mu.RUnlock()

	if !exists {
		logger.Error().
			Str("agent_id", req.AgentId).
			Msg("Rollback için agent bulunamadı")
		return &pb.PostgresRollbackResponse{
			JobId:        req.JobId,
			Status:       pb.JobStatus_JOB_STATUS_FAILED,
			ErrorMessage: "Agent bulunamadı",
		}, nil
	}

	// Rollback komutunu gönder
	command := fmt.Sprintf("rollback_postgres_failover|%s|%s", req.JobId, req.Reason)

	logger.Debug().
		Str("agent_id", req.AgentId).
		Str("command", command).
		Msg("Rollback komutu agent'a gönderiliyor")

	// Timeout ile komut gönder
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	sendDone := make(chan error, 1)
	go func() {
		err := agent.Stream.Send(&pb.ServerMessage{
			Payload: &pb.ServerMessage_Query{
				Query: &pb.Query{
					QueryId: req.JobId,
					Command: command,
				},
			},
		})
		sendDone <- err
	}()

	select {
	case err := <-sendDone:
		if err != nil {
			logger.Error().
				Err(err).
				Str("agent_id", req.AgentId).
				Str("job_id", req.JobId).
				Msg("Rollback komutu gönderilemedi")
			return &pb.PostgresRollbackResponse{
				JobId:        req.JobId,
				Status:       pb.JobStatus_JOB_STATUS_FAILED,
				ErrorMessage: fmt.Sprintf("Rollback komutu gönderilemedi: %v", err),
			}, nil
		}
	case <-ctx.Done():
		logger.Error().
			Str("agent_id", req.AgentId).
			Str("job_id", req.JobId).
			Msg("Rollback komutu gönderme timeout")
		return &pb.PostgresRollbackResponse{
			JobId:        req.JobId,
			Status:       pb.JobStatus_JOB_STATUS_FAILED,
			ErrorMessage: "Rollback komutu gönderme timeout (10s)",
		}, nil
	}

	logger.Info().
		Str("agent_id", req.AgentId).
		Str("job_id", req.JobId).
		Msg("Rollback komutu başarıyla gönderildi")

	return &pb.PostgresRollbackResponse{
		JobId:  req.JobId,
		Status: pb.JobStatus_JOB_STATUS_PENDING,
		Result: "Rollback işlemi başlatıldı",
	}, nil
}

// GetPostgresRollbackInfo PostgreSQL rollback durumunu sorgular
func (s *Server) GetPostgresRollbackInfo(ctx context.Context, req *pb.PostgresRollbackInfoRequest) (*pb.PostgresRollbackInfoResponse, error) {
	logger.Debug().
		Str("agent_id", req.AgentId).
		Msg("PostgreSQL rollback durumu sorgulanıyor")

	// Agent'ı bul
	s.mu.RLock()
	agent, exists := s.agents[req.AgentId]
	s.mu.RUnlock()

	if !exists {
		logger.Warn().
			Str("agent_id", req.AgentId).
			Msg("Rollback info için agent bulunamadı")
		return &pb.PostgresRollbackInfoResponse{
			HasState: false,
		}, nil
	}

	// Rollback durumu sorgu komutunu gönder
	command := "get_postgres_rollback_info"
	queryId := fmt.Sprintf("rollback_info_%d", time.Now().Unix())

	logger.Debug().
		Str("agent_id", req.AgentId).
		Str("query_id", queryId).
		Str("command", command).
		Msg("Rollback info komutu agent'a gönderiliyor")

	// Timeout ile komut gönder
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	sendDone := make(chan error, 1)
	go func() {
		err := agent.Stream.Send(&pb.ServerMessage{
			Payload: &pb.ServerMessage_Query{
				Query: &pb.Query{
					QueryId: queryId,
					Command: command,
				},
			},
		})
		sendDone <- err
	}()

	select {
	case err := <-sendDone:
		if err != nil {
			logger.Error().
				Err(err).
				Str("agent_id", req.AgentId).
				Msg("Rollback info komutu gönderilemedi")
			return &pb.PostgresRollbackInfoResponse{
				HasState: false,
			}, nil
		}
	case <-ctx.Done():
		logger.Error().
			Str("agent_id", req.AgentId).
			Msg("Rollback info komutu gönderme timeout")
		return &pb.PostgresRollbackInfoResponse{
			HasState: false,
		}, nil
	}

	// TODO: Agent'tan gelen response'u beklemek için async handling gerekebilir
	// Şimdilik basic response döndürüyoruz
	logger.Info().
		Str("agent_id", req.AgentId).
		Msg("Rollback info komutu başarıyla gönderildi")

	return &pb.PostgresRollbackInfoResponse{
		HasState: true,
		// Diğer alanlar agent'tan gelen yanıta göre doldurulacak
		// Bu kısım agent response handling sistemi ile tamamlanacak
	}, nil
}

// GetRecentAlarms, dashboard için optimize edilmiş son alarmları getirir
func (s *Server) GetRecentAlarms(ctx context.Context, limit int, onlyUnacknowledged bool) ([]map[string]interface{}, error) {
	// Veritabanı bağlantısını kontrol et
	if err := s.checkDatabaseConnection(); err != nil {
		return nil, fmt.Errorf("veritabanı bağlantı hatası: %v", err)
	}

	// Varsayılan limit kontrolü (dashboard için max 50)
	if limit <= 0 || limit > 50 {
		limit = 20 // Dashboard için varsayılan 20 kayıt
	}

	// Optimize edilmiş sorgu - sadece gerekli alanlar ve index kullanımı
	query := `
		SELECT 
			alarm_id,
			event_id,
			agent_id,
			status,
			metric_name,
			metric_value,
			message,
			severity,
			created_at,
			acknowledged
		FROM alarms`

	var queryParams []interface{}
	if onlyUnacknowledged {
		query += ` WHERE acknowledged = false`
	}

	query += ` ORDER BY created_at DESC LIMIT $1`
	queryParams = append(queryParams, limit)

	logger.Debug().
		Int("limit", limit).
		Bool("only_unacknowledged", onlyUnacknowledged).
		Msg("Recent alarms sorgusu")

	// Sorguyu çalıştır
	rows, err := s.db.QueryContext(ctx, query, queryParams...)
	if err != nil {
		return nil, fmt.Errorf("recent alarm verileri çekilemedi: %v", err)
	}
	defer rows.Close()

	// Sonuçları topla (hafif veri yapısı)
	alarms := make([]map[string]interface{}, 0)
	for rows.Next() {
		var (
			alarmID      string
			eventID      string
			agentID      string
			status       string
			metricName   string
			metricValue  string
			message      string
			severity     string
			createdAt    time.Time
			acknowledged bool
		)

		// Satırı oku
		err := rows.Scan(
			&alarmID,
			&eventID,
			&agentID,
			&status,
			&metricName,
			&metricValue,
			&message,
			&severity,
			&createdAt,
			&acknowledged,
		)
		if err != nil {
			return nil, fmt.Errorf("satır okuma hatası: %v", err)
		}

		// Hafif alarm objesi (dashboard için gerekli minimum alanlar)
		alarm := map[string]interface{}{
			"alarm_id":     alarmID,
			"event_id":     eventID,
			"agent_id":     agentID,
			"status":       status,
			"metric_name":  metricName,
			"metric_value": metricValue,
			"message":      message,
			"severity":     severity,
			"created_at":   createdAt.Format(time.RFC3339),
			"acknowledged": acknowledged,
		}

		alarms = append(alarms, alarm)
	}

	// Satır okuma hatası kontrolü
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("satır okuma hatası: %v", err)
	}

	return alarms, nil
}

// GetAlarmFilterValues, frontend filtreleme için kullanılacak unique değerleri getirir
func (s *Server) GetAlarmFilterValues(ctx context.Context) (map[string][]string, error) {
	// Veritabanı bağlantısını kontrol et
	if err := s.checkDatabaseConnection(); err != nil {
		return nil, fmt.Errorf("veritabanı bağlantı hatası: %v", err)
	}

	// Filtrelenebilir kolonlar
	filterableColumns := []string{
		"agent_id",
		"status",
		"metric_name",
		"severity",
		"database",
	}

	result := make(map[string][]string)

	// Her kolon için unique değerleri çek
	for _, column := range filterableColumns {
		// NULL değerleri hariç tutarak unique değerleri al
		var query string
		if column == "database" {
			// Database kolonu NULL olabilir, NULL'ları hariç tut
			query = fmt.Sprintf(`
				SELECT DISTINCT %s 
				FROM alarms 
				WHERE %s IS NOT NULL AND %s != ''
				ORDER BY %s ASC
			`, column, column, column, column)
		} else {
			// Diğer kolonlar NOT NULL
			query = fmt.Sprintf(`
				SELECT DISTINCT %s 
				FROM alarms 
				WHERE %s IS NOT NULL AND %s != ''
				ORDER BY %s ASC
			`, column, column, column, column)
		}

		rows, err := s.db.QueryContext(ctx, query)
		if err != nil {
			logger.Error().
				Err(err).
				Str("column", column).
				Msg("Alarm filter değerleri çekilemedi")
			continue // Bu kolon için hata olsa da diğerleri için devam et
		}

		var values []string
		for rows.Next() {
			var value string
			if err := rows.Scan(&value); err != nil {
				logger.Error().
					Err(err).
					Str("column", column).
					Msg("Satır okuma hatası")
				continue
			}

			// Boş olmayan değerleri ekle
			if strings.TrimSpace(value) != "" {
				values = append(values, value)
			}
		}
		rows.Close()

		// Satır okuma hatası kontrolü
		if err = rows.Err(); err != nil {
			logger.Error().
				Err(err).
				Str("column", column).
				Msg("Satır okuma hatası")
			continue
		}

		result[column] = values

		logger.Debug().
			Str("column", column).
			Int("value_count", len(values)).
			Msg("Alarm filter değerleri başarıyla çekildi")
	}

	// Ayrıca alarm_id'lerin unique değerlerini de ekle (özel case)
	alarmIDQuery := `
		SELECT DISTINCT alarm_id 
		FROM alarms 
		WHERE alarm_id IS NOT NULL AND alarm_id != ''
		ORDER BY alarm_id ASC
		LIMIT 1000
	`

	rows, err := s.db.QueryContext(ctx, alarmIDQuery)
	if err == nil {
		var alarmIDs []string
		for rows.Next() {
			var alarmID string
			if err := rows.Scan(&alarmID); err != nil {
				continue
			}
			if strings.TrimSpace(alarmID) != "" {
				alarmIDs = append(alarmIDs, alarmID)
			}
		}
		rows.Close()
		result["alarm_id"] = alarmIDs

		logger.Debug().
			Int("alarm_id_count", len(alarmIDs)).
			Msg("Alarm ID filter değerleri başarıyla çekildi")
	}

	logger.Info().
		Int("filter_categories", len(result)).
		Msg("Alarm filter değerleri başarıyla tamamlandı")

	return result, nil
}

func compareValues(a, b interface{}) bool {
	// Nil kontrolü
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	// Numeric değerler için tip dönüşümü yaparak karşılaştır
	aFloat, aIsNumeric := convertToFloat64(a)
	bFloat, bIsNumeric := convertToFloat64(b)

	if aIsNumeric && bIsNumeric {
		// Float karşılaştırması için tolerans kullan (floating point precision)
		return math.Abs(aFloat-bFloat) < 1e-9
	}

	// String karşılaştırması
	if aStr, ok := a.(string); ok {
		if bStr, ok := b.(string); ok {
			return aStr == bStr
		}
		return false
	}

	// Boolean karşılaştırması
	if aBool, ok := a.(bool); ok {
		if bBool, ok := b.(bool); ok {
			return aBool == bBool
		}
		return false
	}

	// Complex types (maps, slices) için reflect.DeepEqual kullan
	return reflect.DeepEqual(a, b)
}

// convertToFloat64, farklı numeric tiplerini float64'e dönüştürür
func convertToFloat64(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int32:
		return float64(val), true
	case int64:
		return float64(val), true
	case uint:
		return float64(val), true
	case uint32:
		return float64(val), true
	case uint64:
		return float64(val), true
	default:
		return 0, false
	}
}

// handleCoordinationRollback koordinasyon rollback işlemini yönetir
func (s *Server) handleCoordinationRollback(targetHost, targetIP, originalMasterHost, originalMasterIP, reason string) {
	logger.Info().
		Str("target_host", targetHost).
		Str("target_ip", targetIP).
		Str("original_master_host", originalMasterHost).
		Str("original_master_ip", originalMasterIP).
		Str("reason", reason).
		Msg("Starting coordination rollback")

	// 1. Target node'u (Fenrir) STANDBY'ye döndür
	if err := s.revertNodeToStandby(targetHost, targetIP, originalMasterHost, originalMasterIP); err != nil {
		logger.Error().
			Err(err).
			Str("target_host", targetHost).
			Msg("Failed to revert node to standby")
		// Hata durumunda alarm gönder
		s.sendSplitBrainAlert(targetHost, originalMasterHost, err)
		return
	}

	logger.Info().
		Str("target_host", targetHost).
		Msg("Coordination rollback completed successfully - node reverted to STANDBY")
}

// revertNodeToStandby bir node'u STANDBY moduna döndürür
func (s *Server) revertNodeToStandby(targetHost, targetIP, newMasterHost, newMasterIP string) error {
	logger.Info().
		Str("target_host", targetHost).
		Str("target_ip", targetIP).
		Str("new_master_host", newMasterHost).
		Str("new_master_ip", newMasterIP).
		Msg("Reverting node to standby mode")

	// Target node'a convert_postgres_to_slave komutu gönder
	convertCommand := fmt.Sprintf("convert_postgres_to_slave|%s|%s|5432|/var/lib/postgresql/15/main|rollback_%d|%s",
		newMasterHost, newMasterIP, time.Now().Unix(), targetHost)

	logger.Info().
		Str("target_host", targetHost).
		Str("command", convertCommand).
		Msg("Sending coordination rollback command")

	// Query gönderme sistemi ile komutu target node'a gönder
	queryID := fmt.Sprintf("coordination_rollback_%d", time.Now().Unix())

	// Timeout context oluştur
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Target node'a komutu gönder
	result, err := s.SendQuery(ctx, targetHost, queryID, convertCommand, "")
	if err != nil {
		return fmt.Errorf("failed to send rollback command to %s: %v", targetHost, err)
	}

	// Sonucu kontrol et - result.Result *anypb.Any tipinde olduğu için string'e çevirmek gerekiyor
	var resultStr string
	if result.Result != nil {
		// Any tipini string'e çevir
		var structValue structpb.Struct
		if err := result.Result.UnmarshalTo(&structValue); err == nil {
			if resultMap := structValue.AsMap(); resultMap != nil {
				if status, ok := resultMap["status"].(string); ok && status != "success" {
					return fmt.Errorf("rollback command failed on %s: status=%s", targetHost, status)
				}
				if resultBytes, err := json.Marshal(resultMap); err == nil {
					resultStr = string(resultBytes)
				}
			}
		}
	}

	logger.Info().
		Str("target_host", targetHost).
		Str("result", resultStr).
		Msg("Coordination rollback command completed successfully")

	return nil
}

// sendSplitBrainAlert split-brain durumu için alarm gönderir
func (s *Server) sendSplitBrainAlert(targetHost, originalMasterHost string, err error) {
	logger.Error().
		Str("target_host", targetHost).
		Str("original_master_host", originalMasterHost).
		Err(err).
		Msg("CRITICAL: Split-brain situation detected!")

	logger.Error().
		Str("target_node", targetHost+" (still PRIMARY)").
		Str("original_master", originalMasterHost+" (reverted to MASTER)").
		Str("rollback_error", err.Error()).
		Msg("MANUAL INTERVENTION REQUIRED!")

	// Koordinasyon rollback event'ini kaydet
	s.logCoordinationRollback(targetHost, originalMasterHost, "revert_to_standby", "failed")

	// Burada alarm sisteminize göre kritik alarm gönderebilirsiniz
	// Örnek: Slack, email, monitoring system, vs.
}

// logCoordinationRollback koordinasyon rollback için özel logging
func (s *Server) logCoordinationRollback(targetHost, originalHost, action, status string) {
	logger.Info().
		Str("event_type", "coordination_rollback").
		Str("target_host", targetHost).
		Str("original_host", originalHost).
		Str("action", action).
		Str("status", status).
		Time("timestamp", time.Now()).
		Msg("Coordination rollback event logged")

	// Database'e kaydet veya monitoring sistemine gönder
	// Bu kısım isteğe bağlı olarak genişletilebilir
}

// ExecutePatroniCommand, Patroni komutunu çalıştırır
func (s *Server) ExecutePatroniCommand(ctx context.Context, req *pb.PatroniCommandRequest) (*pb.PatroniCommandResponse, error) {
	logger.Info().
		Str("agent_id", req.AgentId).
		Str("command", req.Command).
		Str("job_id", req.JobId).
		Msg("ExecutePatroniCommand çağrıldı")

	// Agent'ın bağlı olup olmadığını kontrol et
	s.mu.RLock()
	agent, agentExists := s.agents[req.AgentId]
	s.mu.RUnlock()

	if !agentExists || agent == nil {
		return nil, fmt.Errorf("agent bulunamadı veya bağlantı kapalı: %s", req.AgentId)
	}

	// Unique bir sorgu ID'si oluştur
	queryID := fmt.Sprintf("patroni_cmd_%d", time.Now().UnixNano())

	// Patroni komutunu oluştur
	command := fmt.Sprintf("PATRONI_CMD|%s", req.Command)
	if len(req.Args) > 0 {
		for _, arg := range req.Args {
			command = fmt.Sprintf("%s|%s", command, arg)
		}
	}

	logger.Debug().
		Str("command", command).
		Str("agent_id", req.AgentId).
		Str("query_id", queryID).
		Msg("Patroni komut")

	// Sonuç kanalı oluştur
	resultChan := make(chan *pb.QueryResult, 1)
	s.queryMu.Lock()
	s.queryResult[queryID] = &QueryResponse{
		ResultChan: resultChan,
	}
	s.queryMu.Unlock()

	// Context'in iptal durumunda kaynakları temizle
	defer func() {
		s.queryMu.Lock()
		delete(s.queryResult, queryID)
		s.queryMu.Unlock()
		close(resultChan)
	}()

	// Sorguyu gönder
	err := agent.Stream.Send(&pb.ServerMessage{
		Payload: &pb.ServerMessage_Query{
			Query: &pb.Query{
				QueryId: queryID,
				Command: command,
			},
		},
	})
	if err != nil {
		logger.Error().
			Err(err).
			Str("agent_id", req.AgentId).
			Str("query_id", queryID).
			Msg("Patroni komutu gönderilemedi")
		return nil, fmt.Errorf("sorgu gönderilemedi: %v", err)
	}

	logger.Info().
		Str("agent_id", req.AgentId).
		Str("query_id", queryID).
		Msg("Patroni komutu gönderildi")

	// Cevabı bekle
	select {
	case result := <-resultChan:
		if result == nil {
			logger.Error().
				Str("agent_id", req.AgentId).
				Str("query_id", queryID).
				Msg("Boş Patroni komut yanıtı alındı")
			return nil, fmt.Errorf("boş komut yanıtı alındı")
		}

		logger.Info().
			Str("agent_id", req.AgentId).
			Str("query_id", queryID).
			Msg("Patroni komut yanıtı alındı")

		// Result'ı parse et
		var resultStruct structpb.Struct
		if err := result.Result.UnmarshalTo(&resultStruct); err != nil {
			logger.Error().
				Err(err).
				Str("agent_id", req.AgentId).
				Str("query_id", queryID).
				Msg("Patroni komut yanıtı ayrıştırılamadı")
			return nil, fmt.Errorf("komut yanıtı ayrıştırılamadı: %v", err)
		}

		// JSON string'e çevir
		resultJSON, _ := json.Marshal(resultStruct.AsMap())

		return &pb.PatroniCommandResponse{
			JobId:      req.JobId,
			Status:     pb.JobStatus_JOB_STATUS_COMPLETED,
			Command:    req.Command,
			Args:       req.Args,
			Output:     string(resultJSON),
			ExecutedAt: time.Now().Unix(),
			Duration:   0, // Agent tarafından hesaplanacak
		}, nil

	case <-ctx.Done():
		logger.Error().
			Err(ctx.Err()).
			Str("agent_id", req.AgentId).
			Str("query_id", queryID).
			Msg("Patroni komut yanıtı beklerken timeout")
		return nil, ctx.Err()
	}
}

// GetPatroniClusterStatus, Patroni cluster durumunu getirir
func (s *Server) GetPatroniClusterStatus(ctx context.Context, req *pb.PatroniStatusRequest) (*pb.PatroniStatusResponse, error) {
	logger.Info().
		Str("agent_id", req.AgentId).
		Msg("GetPatroniClusterStatus çağrıldı")

	// Agent'ın bağlı olup olmadığını kontrol et
	s.mu.RLock()
	agent, agentExists := s.agents[req.AgentId]
	s.mu.RUnlock()

	if !agentExists || agent == nil {
		return nil, fmt.Errorf("agent bulunamadı veya bağlantı kapalı: %s", req.AgentId)
	}

	// Unique bir sorgu ID'si oluştur
	queryID := fmt.Sprintf("patroni_status_%d", time.Now().UnixNano())

	// Patroni status komutunu oluştur
	command := "PATRONI_STATUS"

	logger.Debug().
		Str("command", command).
		Str("agent_id", req.AgentId).
		Str("query_id", queryID).
		Msg("Patroni status komut")

	// Sonuç kanalı oluştur
	resultChan := make(chan *pb.QueryResult, 1)
	s.queryMu.Lock()
	s.queryResult[queryID] = &QueryResponse{
		ResultChan: resultChan,
	}
	s.queryMu.Unlock()

	// Context'in iptal durumunda kaynakları temizle
	defer func() {
		s.queryMu.Lock()
		delete(s.queryResult, queryID)
		s.queryMu.Unlock()
		close(resultChan)
	}()

	// Sorguyu gönder
	err := agent.Stream.Send(&pb.ServerMessage{
		Payload: &pb.ServerMessage_Query{
			Query: &pb.Query{
				QueryId: queryID,
				Command: command,
			},
		},
	})
	if err != nil {
		logger.Error().
			Err(err).
			Str("agent_id", req.AgentId).
			Str("query_id", queryID).
			Msg("Patroni status sorgusu gönderilemedi")
		return nil, fmt.Errorf("sorgu gönderilemedi: %v", err)
	}

	logger.Info().
		Str("agent_id", req.AgentId).
		Str("query_id", queryID).
		Msg("Patroni status sorgusu gönderildi")

	// Cevabı bekle
	select {
	case result := <-resultChan:
		if result == nil {
			logger.Error().
				Str("agent_id", req.AgentId).
				Str("query_id", queryID).
				Msg("Boş Patroni status yanıtı alındı")
			return nil, fmt.Errorf("boş status yanıtı alındı")
		}

		logger.Info().
			Str("agent_id", req.AgentId).
			Str("query_id", queryID).
			Msg("Patroni status yanıtı alındı")

		// Result'ı parse et
		var resultStruct structpb.Struct
		if err := result.Result.UnmarshalTo(&resultStruct); err != nil {
			logger.Error().
				Err(err).
				Str("agent_id", req.AgentId).
				Str("query_id", queryID).
				Msg("Patroni status yanıtı ayrıştırılamadı")
			return nil, fmt.Errorf("status yanıtı ayrıştırılamadı: %v", err)
		}

		// JSON string'e çevir
		resultJSON, _ := json.Marshal(resultStruct.AsMap())

		return &pb.PatroniStatusResponse{
			Status:      "success",
			ClusterName: "unknown", // Agent'dan gelecek
			NodeRole:    "unknown", // Agent'dan gelecek
			NodeState:   "unknown", // Agent'dan gelecek
			Output:      string(resultJSON),
			LastUpdated: time.Now().Unix(),
		}, nil

	case <-ctx.Done():
		logger.Error().
			Err(ctx.Err()).
			Str("agent_id", req.AgentId).
			Str("query_id", queryID).
			Msg("Patroni status yanıtı beklerken timeout")
		return nil, ctx.Err()
	}
}

// GetPatroniAvailableCommands, kullanılabilir Patroni komutlarını getirir
func (s *Server) GetPatroniAvailableCommands(ctx context.Context, req *pb.PatroniCommandsRequest) (*pb.PatroniCommandsResponse, error) {
	logger.Info().
		Str("agent_id", req.AgentId).
		Msg("GetPatroniAvailableCommands çağrıldı")

	// Agent'ın bağlı olup olmadığını kontrol et
	s.mu.RLock()
	agent, agentExists := s.agents[req.AgentId]
	s.mu.RUnlock()

	if !agentExists || agent == nil {
		return nil, fmt.Errorf("agent bulunamadı veya bağlantı kapalı: %s", req.AgentId)
	}

	// Unique bir sorgu ID'si oluştur
	queryID := fmt.Sprintf("patroni_commands_%d", time.Now().UnixNano())

	// Patroni commands komutunu oluştur
	command := "PATRONI_COMMANDS"

	logger.Debug().
		Str("command", command).
		Str("agent_id", req.AgentId).
		Str("query_id", queryID).
		Msg("Patroni commands komut")

	// Sonuç kanalı oluştur
	resultChan := make(chan *pb.QueryResult, 1)
	s.queryMu.Lock()
	s.queryResult[queryID] = &QueryResponse{
		ResultChan: resultChan,
	}
	s.queryMu.Unlock()

	// Context'in iptal durumunda kaynakları temizle
	defer func() {
		s.queryMu.Lock()
		delete(s.queryResult, queryID)
		s.queryMu.Unlock()
		close(resultChan)
	}()

	// Sorguyu gönder
	err := agent.Stream.Send(&pb.ServerMessage{
		Payload: &pb.ServerMessage_Query{
			Query: &pb.Query{
				QueryId: queryID,
				Command: command,
			},
		},
	})
	if err != nil {
		logger.Error().
			Err(err).
			Str("agent_id", req.AgentId).
			Str("query_id", queryID).
			Msg("Patroni commands sorgusu gönderilemedi")
		return nil, fmt.Errorf("sorgu gönderilemedi: %v", err)
	}

	logger.Info().
		Str("agent_id", req.AgentId).
		Str("query_id", queryID).
		Msg("Patroni commands sorgusu gönderildi")

	// Cevabı bekle
	select {
	case result := <-resultChan:
		if result == nil {
			logger.Error().
				Str("agent_id", req.AgentId).
				Str("query_id", queryID).
				Msg("Boş Patroni commands yanıtı alındı")
			return nil, fmt.Errorf("boş commands yanıtı alındı")
		}

		logger.Info().
			Str("agent_id", req.AgentId).
			Str("query_id", queryID).
			Msg("Patroni commands yanıtı alındı")

		// Result'ı parse et
		var resultStruct structpb.Struct
		if err := result.Result.UnmarshalTo(&resultStruct); err != nil {
			logger.Error().
				Err(err).
				Str("agent_id", req.AgentId).
				Str("query_id", queryID).
				Msg("Patroni commands yanıtı ayrıştırılamadı")
			return nil, fmt.Errorf("commands yanıtı ayrıştırılamadı: %v", err)
		}

		// Varsayılan komutlar listesi (agent'dan gelecek gerçek komutlar ile değiştirilecek)
		commands := []*pb.PatroniCommand{
			{
				Name:         "list",
				Command:      "patronictl list",
				Args:         []string{},
				Description:  "List all cluster members",
				Category:     "cluster",
				RequiresAuth: false,
			},
			{
				Name:         "switchover",
				Command:      "patronictl switchover",
				Args:         []string{"--cluster", "--master", "--candidate"},
				Description:  "Perform a switchover",
				Category:     "failover",
				RequiresAuth: true,
			},
			{
				Name:         "failover",
				Command:      "patronictl failover",
				Args:         []string{"--cluster", "--master", "--candidate"},
				Description:  "Perform a failover",
				Category:     "failover",
				RequiresAuth: true,
			},
			{
				Name:         "reinit",
				Command:      "patronictl reinit",
				Args:         []string{"--cluster", "--member"},
				Description:  "Reinitialize a cluster member",
				Category:     "maintenance",
				RequiresAuth: true,
			},
		}

		return &pb.PatroniCommandsResponse{
			Status:   "success",
			Commands: commands,
		}, nil

	case <-ctx.Done():
		logger.Error().
			Err(ctx.Err()).
			Str("agent_id", req.AgentId).
			Str("query_id", queryID).
			Msg("Patroni commands yanıtı beklerken timeout")
		return nil, ctx.Err()
	}
}

// ExecuteMongoCommand executes a generic MongoDB command on the agent
func (s *Server) ExecuteMongoCommand(ctx context.Context, agentID, commandName string, params map[string]interface{}) (map[string]interface{}, error) {
	// Agent'ın bağlı olup olmadığını kontrol et
	s.mu.RLock()
	agent, agentExists := s.agents[agentID]
	s.mu.RUnlock()

	if !agentExists || agent == nil {
		return nil, fmt.Errorf("agent bulunamadı veya bağlantı kapalı: %s", agentID)
	}

	// Command string oluştur
	command := commandName
	if params != nil {
		if paramsJSON, err := json.Marshal(params); err == nil {
			command = fmt.Sprintf("%s|%s", commandName, string(paramsJSON))
		}
	}
	
	queryID := fmt.Sprintf("mongo_%s_%d", commandName, time.Now().UnixNano())

	logger.Debug().
		Str("command", command).
		Str("agent_id", agentID).
		Str("query_id", queryID).
		Interface("params", params).
		Msg("MongoDB komutu çalıştırılıyor")

	// Sonuç kanalı oluştur
	resultChan := make(chan *pb.QueryResult, 1)
	s.queryMu.Lock()
	s.queryResult[queryID] = &QueryResponse{
		ResultChan: resultChan,
	}
	s.queryMu.Unlock()

	// Context'in iptal durumunda kaynakları temizle
	defer func() {
		s.queryMu.Lock()
		delete(s.queryResult, queryID)
		s.queryMu.Unlock()
		close(resultChan)
	}()

	// Sorguyu gönder
	if err := agent.Stream.Send(&pb.ServerMessage{
		Payload: &pb.ServerMessage_Query{
			Query: &pb.Query{
				QueryId: queryID,
				Command: command,
			},
		},
	}); err != nil {
		return nil, fmt.Errorf("sorgu gönderilemedi: %v", err)
	}

	logger.Info().
		Str("agent_id", agentID).
		Str("query_id", queryID).
		Str("command_name", commandName).
		Msg("MongoDB komutu için sorgu gönderildi")

	// Cevabı bekle
	select {
	case result := <-resultChan:
		// Sonuç geldi
		if result == nil {
			return nil, fmt.Errorf("null sorgu sonucu alındı")
		}

		logger.Debug().
			Str("query_id", queryID).
			Str("type_url", result.Result.TypeUrl).
			Msg("Agent'tan yanıt alındı")

		// Struct olarak ayrıştır
		var resultStruct structpb.Struct
		if err := result.Result.UnmarshalTo(&resultStruct); err != nil {
			logger.Error().
				Err(err).
				Str("query_id", queryID).
				Msg("Struct ayrıştırma hatası")
			return nil, fmt.Errorf("sonuç ayrıştırma hatası: %v", err)
		}

		// Debug: Tüm field'ları log'la
		logger.Debug().
			Str("query_id", queryID).
			Interface("fields", resultStruct.Fields).
			Msg("Agent'tan gelen response fields")

		// Status kontrolü
		statusValue, ok := resultStruct.Fields["status"]
		if !ok {
			logger.Error().
				Str("query_id", queryID).
				Msg("Status field bulunamadı")
			return nil, fmt.Errorf("agent hatası: Status field yok")
		}
		
		statusStr := statusValue.GetStringValue()
		logger.Debug().
			Str("query_id", queryID).
			Str("status", statusStr).
			Msg("Agent status")
			
		if statusStr != "success" {
			errorMsg := "Bilinmeyen hata"
			if messageValue, exists := resultStruct.Fields["message"]; exists {
				errorMsg = messageValue.GetStringValue()
			}
			logger.Error().
				Str("query_id", queryID).
				Str("status", statusStr).
				Str("error", errorMsg).
				Msg("Agent başarısız status döndürdü")
			return nil, fmt.Errorf("agent hatası: %s", errorMsg)
		}

		// Result field'ından data'yı çıkar
		var resultData map[string]interface{}
		if resultValue, exists := resultStruct.Fields["result"]; exists {
			if resultValue.GetStructValue() != nil {
				resultData = make(map[string]interface{})
				for k, v := range resultValue.GetStructValue().Fields {
					resultData[k] = v.AsInterface()
				}
			}
		}

		logger.Info().
			Str("query_id", queryID).
			Str("command_name", commandName).
			Msg("MongoDB komutu başarıyla tamamlandı")
		
		return resultData, nil

	case <-ctx.Done():
		logger.Error().
			Err(ctx.Err()).
			Str("agent_id", agentID).
			Str("query_id", queryID).
			Str("command_name", commandName).
			Msg("MongoDB komutu yanıtı beklerken timeout")
		return nil, ctx.Err()
	}
}
