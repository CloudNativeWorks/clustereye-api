# 🛡️ ClusterEye API Güvenlik Rehberi

## 🚨 Kritik Güvenlik Güncellemeleri Uygulandı

Bu dokümanda ClusterEye API'sine uygulanan güvenlik önlemleri ve konfigürasyon adımları açıklanmaktadır.

## ✅ Uygulanan Güvenlik Önlemleri

### 1. **Authentication & Authorization**
- ✅ Tüm kritik endpoint'lere JWT authentication eklendi
- ✅ Admin-only endpoint'ler için AdminMiddleware eklendi
- ✅ JWT secret key environment variable'dan alınıyor

### 2. **Rate Limiting**
- ✅ IP bazlı rate limiting (dakikada 120 istek)
- ✅ Aşırı istek yapan IP'ler 15 dakika bloklanıyor
- ✅ Otomatik temizlik mekanizması

### 3. **Security Headers**
- ✅ X-Content-Type-Options: nosniff
- ✅ X-Frame-Options: DENY
- ✅ X-XSS-Protection: 1; mode=block
- ✅ Content-Security-Policy
- ✅ Cache-Control headers

### 4. **Request Logging**
- ✅ Tüm istekler loglanıyor
- ✅ Şüpheli istekler (4xx, 5xx) özel olarak işaretleniyor
- ✅ IP, User-Agent, süre bilgileri kaydediliyor

## 🔧 Konfigürasyon Adımları

### 1. Environment Variables Ayarı

```bash
# JWT Secret Key ayarla (ÜRETİMDE MUTLAKA DEĞİŞTİR!)
export JWT_SECRET_KEY="your-super-secret-jwt-key-change-this-in-production-32chars-min"

# Veya .env dosyası kullan
cp env.example .env
nano .env
```

### 2. Güvenli JWT Secret Key Oluşturma

```bash
# 32 karakterlik rastgele key oluştur
openssl rand -base64 32

# Veya
head -c 32 /dev/urandom | base64
```

### 3. Rate Limiting Ayarları

Varsayılan: **120 istek/dakika**

Değiştirmek için `cmd/api/main.go` dosyasında:
```go
// Rate limiting middleware - dakikada 60 istek
router.Use(api.RateLimitMiddleware(60, time.Minute))
```

## 🔒 Korunan Endpoint'ler

### Artık Authentication Gerektiren Endpoint'ler:

#### Agent İşlemleri
- `GET /api/v1/agents`
- `POST /api/v1/agents/:agent_id/query`
- `POST /api/v1/agents/:agent_id/metrics`
- Tüm agent log ve analiz endpoint'leri

#### Sistem Durumu
- `GET /api/v1/status/postgres`
- `GET /api/v1/status/mongo`
- `GET /api/v1/status/mssql`
- `GET /api/v1/status/agents`
- `GET /api/v1/status/nodeshealth`
- `GET /api/v1/status/alarms`

#### Job İşlemleri
- `POST /api/v1/jobs`
- `GET /api/v1/jobs`
- `GET /api/v1/jobs/:job_id`
- Tüm PostgreSQL/MongoDB job endpoint'leri

#### Metrikler
- `GET /api/v1/metrics/*`
- `GET /api/v1/mssql/*`
- Tüm PostgreSQL/MongoDB metrik endpoint'leri

#### Admin-Only Endpoint'ler
- `GET /api/v1/debug/*`
- `POST /api/v1/debug/*`

## 🚫 Hala Açık Olan Endpoint'ler

- `POST /api/v1/login` - Login endpoint'i açık kalmalı

## 📊 Rate Limiting Detayları

### Varsayılan Limitler:
- **120 istek/dakika** per IP
- Aşım durumunda **15 dakika** blok
- Otomatik temizlik: **5 dakika** aralıklarla

### Blok Durumu:
```json
{
  "error": "Too many requests",
  "message": "Rate limit exceeded. Please try again later."
}
```

## 🔍 Log Monitoring

### Şüpheli Aktivite Logları:
```bash
# Rate limit aşımları
grep "Rate limit exceeded" /var/log/clustereye/app.log

# Authentication hataları
grep "Authorization token is required" /var/log/clustereye/app.log

# Admin erişim denemeleri
grep "Admin access required" /var/log/clustereye/app.log

# Şüpheli istekler (4xx, 5xx)
grep "Suspicious request detected" /var/log/clustereye/app.log
```

## ⚡ Acil Durum Prosedürleri

### Saldırı Durumunda:

1. **Rate limit'i düşür:**
   ```bash
   # Servisi durdur
   systemctl stop clustereye-api
   
   # Rate limit'i 10/dakika yap
   # main.go'da değiştir: RateLimitMiddleware(10, time.Minute)
   
   # Servisi başlat
   systemctl start clustereye-api
   ```

2. **Belirli IP'yi blokla (iptables):**
   ```bash
   iptables -A INPUT -s SALDIRGAN_IP -j DROP
   ```

3. **Nginx/Apache seviyesinde blokla:**
   ```nginx
   # nginx.conf
   deny SALDIRGAN_IP;
   ```

## 🔄 Güvenlik Güncellemeleri

### Sonraki Adımlar:
- [ ] HTTPS zorlaması
- [ ] API key authentication
- [ ] IP whitelist/blacklist
- [ ] Brute force protection
- [ ] SQL injection koruması
- [ ] Input validation middleware

## 📞 Güvenlik İhlali Bildirimi

Güvenlik açığı tespit ederseniz:
1. Hemen sistem yöneticisine bildirin
2. Logları kaydedin
3. Etkilenen endpoint'leri geçici olarak kapatın

---

**⚠️ ÖNEMLİ:** Bu güvenlik önlemleri temel koruma sağlar. Üretim ortamında ek güvenlik katmanları (WAF, DDoS koruması, SSL/TLS) gereklidir. 