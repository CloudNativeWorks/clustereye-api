# MongoDB Active Queries Implementation

MongoDB'de active query'leri agent'lardan çekip ön yüzde göstermek için aşağıdaki implementasyon tamamlanmıştır.

## Yapılan Değişiklikler

### 1. Server Tarafı - Metadata İşleme

**Dosya:** `internal/server/server.go`

- `SendMetrics` fonksiyonuna MongoDB active operations metadata işleme eklendi
- `processActiveOperationsMetadata` fonksiyonu eklendi
- `writeActiveOperationToInfluxDB` fonksiyonu eklendi

```go
// MongoDB active operations metadata'sını kontrol et ve işle
if activeOperationsJSON, exists := batch.Metadata["active_operations"]; exists && batch.MetricType == "mongodb_database" {
    if err := s.processActiveOperationsMetadata(ctx, batch.AgentId, activeOperationsJSON); err != nil {
        // Error handling...
    }
}
```

### 2. API Endpoint'leri

**Dosya:** `internal/api/handlers.go`

MongoDB performance group'una yeni endpoint eklendi:
```go
performance.GET("/active-queries-details", getMongoDBActiveQueriesDetails(server))
```

**Dosya:** `internal/api/mongodb_metrics.go`

`getMongoDBActiveQueriesDetails` fonksiyonu eklendi:
- Agent ID, database ve time range filtreleri destekler
- InfluxDB'den `mongodb_active_operation` measurement'ından veri çeker
- Structured response döner

### 3. Veri Yapısı

MongoDB Active Operation struct'ı:
```go
type MongoActiveOperation struct {
    OperationID     string          `json:"operation_id,omitempty"`
    OpType          string          `json:"op_type"`
    Namespace       string          `json:"namespace"`
    Command         string          `json:"command,omitempty"`
    CommandDetails  json.RawMessage `json:"command_details,omitempty"`
    
    // Truncated command handling
    CommandTruncated     bool            `json:"command_truncated,omitempty"`
    CommandType          string          `json:"command_type,omitempty"`
    TruncatedContent     json.RawMessage `json:"truncated_content,omitempty"`
    InferredCommandType  string          `json:"inferred_command_type,omitempty"`
    PlanSummary          string          `json:"plan_summary,omitempty"`
    Note                 string          `json:"note,omitempty"`
    
    DurationSeconds float64         `json:"duration_seconds"`
    Database        string          `json:"database"`
    Collection      string          `json:"collection,omitempty"`
    Client          string          `json:"client,omitempty"`
    
    // 🆕 Detaylı client bilgileri
    ClientDetails   json.RawMessage `json:"client_details,omitempty"`
    
    ConnectionID    int64           `json:"connection_id,omitempty"`
    ThreadID        string          `json:"thread_id,omitempty"`
    WaitingForLock  bool            `json:"waiting_for_lock"`  // ← omitempty kaldırıldı, her zaman gösterilsin
    LockType        string          `json:"lock_type,omitempty"`
    Timestamp       time.Time       `json:"timestamp"`
}
```

## API Kullanımı

### Active Queries Details Endpoint

```
GET /api/v1/metrics/mongodb/performance/active-queries-details
```

**Query Parameters:**
- `agent_id` (optional): Specific agent ID
- `database` (optional): Database filter
- `range` (optional, default: "10m"): Time range
- `limit` (optional, default: "50"): Result limit

**Response:**
```json
{
    "status": "success",
    "data": [
        {
            "operation_id": "-690995983",
            "op_type": "getmore",
            "namespace": "a101_hangfire_preprod.hangfire.jobGraph",
            "command": "getMore",
            "command_details": {
                "cursor_id": "12345678901234567890",
                "batch_size": 1000
            },
            "command_truncated": false,
            "command_type": "getMore",
            "inferred_command_type": "cursor_operation",
            "plan_summary": "IXSCAN { _id: 1 }",
            "note": "Long running cursor operation",
            "duration_seconds": 0.75,
            "database": "a101_hangfire_preprod",
            "collection": "hangfire.jobGraph",
            "client": "192.168.1.100:54321",
            "client_details": {
                "application": "HangfireServer",
                "driver": "MongoDB.Driver",
                "version": "2.19.0",
                "os": "Linux 5.4.0-74-generic",
                "platform": ".NET 6.0.0"
            },
            "connection_id": 123,
            "thread_id": "thread-456",
            "waiting_for_lock": false,
            "lock_type": "r",
            "timestamp": "2024-01-15T10:30:00Z"
        }
    ]
}
```

## InfluxDB Measurement

**Measurement:** `mongodb_active_operation`

**Tags:**
- `agent_id`: Agent identifier
- `database`: Database name
- `collection`: Collection name
- `op_type`: Operation type (query, insert, update, delete, etc.)
- `namespace`: Full namespace (database.collection)
- `client`: Client connection info
- `lock_type`: Lock type if waiting for lock

**Fields:**
- `operation_id`: Unique operation identifier
- `connection_id`: Connection ID
- `duration`: Operation duration in seconds
- `command`: Command being executed
- `command_details`: JSON string containing detailed command information (cursor_id, batch_size, etc.)
- `command_truncated`: Boolean indicating if command was truncated by MongoDB
- `command_type`: Type of MongoDB command (find, getMore, insert, etc.)
- `truncated_content`: JSON string containing truncated command content
- `inferred_command_type`: Inferred command type when command is truncated
- `plan_summary`: Query execution plan summary
- `note`: Additional notes about the operation
- `client_details`: JSON string containing detailed client information (application, driver, version, etc.)
- `thread_id`: Thread identifier
- `waiting_for_lock`: Boolean indicating if waiting for lock (always present, not omitted when false)

## Agent Tarafında Yapılması Gerekenler

Agent'ın MongoDB active operations collect etmesi için aşağıdaki işlemler yapılmalıdır:

### 1. MongoDB currentOp() Komutu

Agent'da MongoDB'ye bağlanıp `currentOp()` komutunu çalıştırmalı:

```javascript
db.currentOp({
    "active": true,
    "secs_running": { "$gte": 0 }
})
```

### 2. Veri Dönüşümü

currentOp() sonucunu aşağıdaki formatta metadata olarak server'a gönderilmeli:

```json
{
    "metric_type": "mongodb_database",
    "metadata": {
        "active_operations": "[{\"operation_id\":\"-690995983\",\"op_type\":\"getmore\",\"namespace\":\"a101_hangfire_preprod.hangfire.jobGraph\",\"database\":\"a101_hangfire_preprod\",\"collection\":\"hangfire.jobGraph\",\"duration_seconds\":0.75,\"client\":\"192.168.1.100:54321\",\"client_details\":{\"application\":\"HangfireServer\",\"driver\":\"MongoDB.Driver\",\"version\":\"2.19.0\",\"os\":\"Linux 5.4.0-74-generic\",\"platform\":\".NET 6.0.0\"},\"connection_id\":123,\"thread_id\":\"thread-456\",\"waiting_for_lock\":false,\"lock_type\":\"r\",\"command\":\"getMore\",\"command_details\":{\"cursor_id\":\"12345678901234567890\",\"batch_size\":1000},\"command_truncated\":false,\"command_type\":\"getMore\",\"inferred_command_type\":\"cursor_operation\",\"plan_summary\":\"IXSCAN { _id: 1 }\",\"note\":\"Long running cursor operation\",\"timestamp\":\"2024-01-15T10:30:00Z\"}]"
    }
}
```

### 3. Örnek Agent Kodu (Pseudo-code)

```go
func collectMongoActiveOperations(mongoClient *mongo.Client) ([]map[string]interface{}, error) {
    // Run currentOp command
    result := mongoClient.Database("admin").RunCommand(context.Background(), bson.D{
        {"currentOp", 1},
        {"active", true},
    })
    
    var currentOp struct {
        Inprog []bson.M `bson:"inprog"`
    }
    
    if err := result.Decode(&currentOp); err != nil {
        return nil, err
    }
    
    operations := make([]map[string]interface{}, 0)
    
    for _, op := range currentOp.Inprog {
        operation := map[string]interface{}{
            "operation_id": fmt.Sprintf("%v", op["opid"]),
            "op_type": op["op"],
            "namespace": op["ns"],
            "duration_seconds": float64(op["secs_running"].(int32)),
            "client": op["client"],
            "connection_id": op["connectionId"],
            "thread_id": op["desc"],
            "waiting_for_lock": op["waitingForLock"],
            "lock_type": op["lockType"],
            "command": fmt.Sprintf("%v", op["command"]),
            "timestamp": time.Now().Format(time.RFC3339Nano),
        }
        
        // 🆕 Client details - detaylı client bilgileri
        if clientInfo, exists := op["clientMetadata"]; exists {
            clientDetails := map[string]interface{}{}
            if clientMap, ok := clientInfo.(bson.M); ok {
                if application, exists := clientMap["application"]; exists {
                    clientDetails["application"] = fmt.Sprintf("%v", application)
                }
                if driver, exists := clientMap["driver"]; exists {
                    if driverMap, ok := driver.(bson.M); ok {
                        if name, exists := driverMap["name"]; exists {
                            clientDetails["driver"] = fmt.Sprintf("%v", name)
                        }
                        if version, exists := driverMap["version"]; exists {
                            clientDetails["version"] = fmt.Sprintf("%v", version)
                        }
                    }
                }
                if os, exists := clientMap["os"]; exists {
                    if osMap, ok := os.(bson.M); ok {
                        if osType, exists := osMap["type"]; exists {
                            if osName, exists := osMap["name"]; exists {
                                clientDetails["os"] = fmt.Sprintf("%v %v", osType, osName)
                            }
                        }
                    }
                }
                if platform, exists := clientMap["platform"]; exists {
                    clientDetails["platform"] = fmt.Sprintf("%v", platform)
                }
            }
            if len(clientDetails) > 0 {
                operation["client_details"] = clientDetails
            }
        }
        
        // Command details ve truncated handling
        if commandMap, ok := op["command"].(bson.M); ok {
            commandDetails := make(map[string]interface{})
            
            // Command type belirleme
            operation["command_type"] = op["op"]
            
            // Truncated command handling
            if truncated, exists := op["truncated"]; exists {
                operation["command_truncated"] = truncated
                if truncated == true {
                    // Truncated content'i ayrı olarak sakla
                    operation["truncated_content"] = commandMap
                    
                    // Command type'ı infer et
                    if op["op"] == "query" {
                        operation["inferred_command_type"] = "find_operation"
                    } else if op["op"] == "getmore" {
                        operation["inferred_command_type"] = "cursor_operation"
                    }
                    
                    // Plan summary varsa ekle
                    if planSummary, exists := op["planSummary"]; exists {
                        operation["plan_summary"] = fmt.Sprintf("%v", planSummary)
                    }
                    
                    // Note ekle
                    operation["note"] = "Command truncated by MongoDB"
                }
            }
            
            // Normal command details
            if op["op"] == "getmore" {
                if cursorId, exists := commandMap["getMore"]; exists {
                    commandDetails["cursor_id"] = fmt.Sprintf("%v", cursorId)
                }
                if batchSize, exists := commandMap["batchSize"]; exists {
                    commandDetails["batch_size"] = batchSize
                }
            } else if op["op"] == "query" {
                if filter, exists := commandMap["filter"]; exists {
                    commandDetails["filter"] = filter
                }
                if projection, exists := commandMap["projection"]; exists {
                    commandDetails["projection"] = projection
                }
            }
            
            // Diğer command tipleri için detaylar eklenebilir
            if len(commandDetails) > 0 {
                operation["command_details"] = commandDetails
            }
        }
        
        // Parse namespace to extract database and collection
        if ns, ok := op["ns"].(string); ok {
            parts := strings.SplitN(ns, ".", 2)
            if len(parts) == 2 {
                operation["database"] = parts[0]
                operation["collection"] = parts[1]
            }
        }
        
        operations = append(operations, operation)
    }
    
    return operations, nil
}
```

## Test Etme

1. Agent'ın MongoDB'ye bağlandığından emin olun
2. Agent'ın active operations metadata'sını server'a gönderdiğini kontrol edin
3. API endpoint'ini test edin:
   ```bash
   curl "http://localhost:8080/api/v1/metrics/mongodb/performance/active-queries-details?agent_id=your_agent_id"
   ```

## TTL (Time To Live)

Active operations verileri InfluxDB'de 10 dakika TTL ile saklanır, böylece eski veriler otomatik olarak temizlenir.

## Yeni Truncated Command Alanlarının Faydaları

### 🆕 Yeni Alanların Faydaları

#### Truncated Command Handling

MongoDB'de uzun komutlar `currentOp()` tarafından kesilir (truncate edilir). İlgili alanlar bu durumu ele alır:

#### Client Details

MongoDB client bilgileri detaylı olarak toplanır ve analiz edilir:

**Truncated Command Alanları:**
- **`command_truncated`**: Komutun kesilip kesilmediğini gösterir
- **`command_type`**: MongoDB command tipini belirtir (find, getMore, insert, etc.)
- **`truncated_content`**: Kesilen komut içeriğini JSON olarak saklar
- **`inferred_command_type`**: Kesilen komutlar için tahmin edilen tip
- **`plan_summary`**: Query execution plan özeti
- **`note`**: Operasyon hakkında ek notlar

**Client Details Alanları:**
- **`client_details`**: Detaylı client bilgileri (application, driver, version, os, platform)
- **`waiting_for_lock`**: Lock bekleme durumu (artık her zaman gösterilir, false olsa bile)

### Kullanım Senaryoları

1. **Uzun Query Analizi**: Kesilen komutlarda bile temel bilgileri görüntüleme
2. **Performance Debugging**: Plan summary ile slow query'leri analiz etme
3. **Operation Categorization**: Command type'a göre operasyonları kategorize etme
4. **Alert Generation**: Belirli command type'larda threshold'ları aşan operasyonlar için alarm
5. **Client Analysis**: Hangi uygulamaların/driver'ların hangi operasyonları yaptığını izleme
6. **Driver Version Tracking**: Eski driver versiyonlarını tespit etme
7. **Lock Monitoring**: Lock bekleme durumlarını her zaman görebilme (false değerleri dahil)

### Frontend Gösterimi

```javascript
// Truncated command örneği
if (operation.command_truncated) {
    display = `${operation.command_type} (truncated)`;
    tooltip = operation.note + "\nPlan: " + operation.plan_summary;
} else {
    display = operation.command;
}

// Client details gösterimi
if (operation.client_details) {
    const clientInfo = JSON.parse(operation.client_details);
    clientDisplay = `${clientInfo.application || 'Unknown'} (${clientInfo.driver} ${clientInfo.version})`;
    clientTooltip = `Driver: ${clientInfo.driver}\nVersion: ${clientInfo.version}\nOS: ${clientInfo.os}\nPlatform: ${clientInfo.platform}`;
}

// Lock status her zaman gösterilir
lockStatus = operation.waiting_for_lock ? "🔒 Waiting" : "✅ No Lock";
```

## Notlar

- Bu implementasyon PostgreSQL active queries implementasyonuna benzer şekilde tasarlanmıştır
- InfluxDB measurement name: `mongodb_active_operation`
- Mevcut `mongodb_performance` measurement'ındaki `active_queries_count` field'ı ile uyumlu çalışır
- Frontend'de PostgreSQL active queries benzeri bir interface ile gösterilebilir
- Yeni truncated command alanları MongoDB'nin command truncation davranışını destekler