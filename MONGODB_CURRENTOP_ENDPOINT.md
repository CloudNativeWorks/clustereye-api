# MongoDB currentOp Endpoint

Bu endpoint, MongoDB agent'larına `db.currentOp()` komutunu göndererek aktif operasyonları getirir.

## Endpoint Bilgileri

**URL:** `POST /api/v1/agents/{agent_id}/mongo/currentop`

**Authentication:** Bearer Token gerekli

**Content-Type:** `application/json`

## Request Parametreleri

### Path Parametreleri
- `agent_id` (string, required): Hedef agent'ın ID'si

### Body Parametreleri
```json
{
  "secs_running": 0.1  // Minimum çalışma süresi (saniye), opsiyonel, default: 0.1
}
```

- `secs_running` (float, optional): MongoDB'de en az bu kadar süre çalışan operasyonları getirir
  - Default değer: 0.1 saniye
  - Kullanıcı tarafından özelleştirilebilir
  - 0 veya negatif değer verilirse 0.1 kullanılır

## Örnek Kullanım

### 1. Default Parametrelerle (0.1 saniye ve üzeri)

```bash
curl -X POST "http://localhost:8080/api/v1/agents/agent_your-mongo-server/mongo/currentop" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{}'
```

### 2. Özel Süre Parametresi ile (1 saniye ve üzeri)

```bash
curl -X POST "http://localhost:8080/api/v1/agents/agent_your-mongo-server/mongo/currentop" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "secs_running": 1.0
  }'
```

### 3. Uzun Çalışan Operasyonlar için (10 saniye ve üzeri)

```bash
curl -X POST "http://localhost:8080/api/v1/agents/agent_your-mongo-server/mongo/currentop" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "secs_running": 10.0
  }'
```

## Response Format

### Başarılı Yanıt (200 OK)
```json
{
  "status": "success",
  "agent_id": "agent_your-mongo-server",
  "query_id": "mongo_currentop_1642234567890123456",
  "secs_running": 0.1,
  "result": {
    "inprog": [
      {
        "opid": "12345",
        "active": true,
        "secs_running": 2.5,
        "op": "query",
        "ns": "myapp.users",
        "command": {
          "find": "users",
          "filter": {"status": "active"}
        },
        "client": "192.168.1.100:54321",
        "connectionId": 123,
        "desc": "conn123",
        "waitingForLock": false,
        "lockType": "r"
      }
    ]
  }
}
```

### Hata Yanıtları

#### Agent Bulunamadı (404 Not Found)
```json
{
  "status": "error",
  "error": "Agent bulunamadı veya bağlantı kapalı"
}
```

#### Geçersiz Agent ID (400 Bad Request)
```json
{
  "status": "error",
  "error": "agent_id parametresi gerekli"
}
```

#### Zaman Aşımı (504 Gateway Timeout)
```json
{
  "status": "error",
  "error": "MongoDB currentOp komutu zaman aşımına uğradı"
}
```

#### Sunucu Hatası (500 Internal Server Error)
```json
{
  "status": "error",
  "error": "MongoDB currentOp komutu çalıştırılırken hata oluştu: [hata detayı]"
}
```

## Agent Tarafında Yapılması Gerekenler

Agent'ın bu endpoint'i desteklemesi için `MONGO_CURRENTOP` komutunu işleyebilmesi gerekir:

### Komut Formatı
Agent'a gönderilen komut formatı: `MONGO_CURRENTOP|{secs_running}`

Örnek: `MONGO_CURRENTOP|0.1`

### MongoDB Komutu
Agent'ın çalıştırması gereken MongoDB komutu:

```javascript
db.currentOp({
  "active": true,
  "secs_running": { "$gte": 0.1 },
  "ns": { "$not": /admin\.\$cmd/ }
})
```

### Parametre Açıklaması
- `active: true`: Sadece aktif operasyonları getir
- `secs_running: {"$gte": X}`: X saniye ve üzeri çalışan operasyonları getir
- `ns: {"$not": /admin\.\$cmd/}`: Admin komutlarını hariç tut

## Kullanım Senaryoları

1. **Performans İzleme**: Uzun süren sorguları tespit etmek için
2. **Debugging**: Hangi operasyonların çalıştığını görmek için  
3. **Kapasite Planlama**: Sistem yükünü analiz etmek için
4. **Sorun Giderme**: Lock bekleyen operasyonları bulmak için

## Güvenlik Notları

- Endpoint authentication gerektirir
- Agent'a gönderilen komut MongoDB admin veritabanında çalıştırılır
- Sadece yetkili kullanıcılar bu endpoint'i kullanabilir
- Timeout süresi 30 saniye olarak ayarlanmıştır