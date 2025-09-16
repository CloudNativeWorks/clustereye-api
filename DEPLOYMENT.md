# ClusterEye Kurulum Rehberi

Bu rehber, ClusterEye API projesini müşterilere kolayca kurabilmek için hazırlanmıştır.

## 🚀 Hızlı Kurulum

### Otomatik Kurulum (Önerilen)

En basit kurulum yöntemi:

```bash
# Projeyi klonlayın
git clone https://github.com/sefaphlvn/clustereye-test.git
cd clustereye-test

# Otomatik deployment scriptini çalıştırın
./deploy.sh
```

Bu script:
- ✅ Tüm gereksinimleri kontrol eder
- ✅ Güvenli şifreler oluşturur
- ✅ Tüm servisleri yapılandırır
- ✅ Veritabanını initialize eder
- ✅ Web arayüzünü hazırlar

### Manual Kurulum

```bash
# 1. Environment dosyasını oluşturun
cp .env.example .env

# 2. .env dosyasını düzenleyin (güvenli şifreler kullanın)
nano .env

# 3. Servisleri başlatın
docker-compose up -d

# 4. Servislerin hazır olmasını bekleyin
docker-compose logs -f
```

## 🔧 Servis Yapılandırması

### Ana Servisler

| Servis | Port | Açıklama |
|--------|------|----------|
| **Nginx** | 80, 443 | Reverse proxy ve frontend |
| **ClusterEye API** | 8080 | Go backend API |
| **PostgreSQL** | 5432 | Ana veritabanı |
| **InfluxDB** | 8086 | Metrik veritabanı |
| **Grafana** | 3000 | İzleme dashboard'u (opsiyonel) |

### Erişim Adresleri

- 🌐 **Web Arayüzü**: http://localhost
- 📡 **API Endpoint**: http://localhost/api
- 📈 **Grafana**: http://localhost:3000
- 💾 **InfluxDB UI**: http://localhost:8086

## 🔐 Güvenlik Yapılandırması

### SSL/HTTPS Etkinleştirme

1. SSL sertifikalarınızı `docker/ssl/` klasörüne koyun:
   ```
   docker/ssl/cert.pem
   docker/ssl/key.pem
   ```

2. `.env` dosyasında HTTPS'i etkinleştirin:
   ```bash
   ENABLE_HTTPS=true
   SSL_CERT_PATH=/app/ssl/cert.pem
   SSL_KEY_PATH=/app/ssl/key.pem
   ```

3. Nginx konfigürasyonunda HTTPS bloğunu aktifleştirin:
   ```bash
   # docker/nginx/conf.d/clustereye.conf dosyasında
   # HTTPS server bloğunun yorumunu kaldırın
   ```

4. Servisleri yeniden başlatın:
   ```bash
   docker-compose restart nginx
   ```

### Güvenlik Kontrol Listesi

- [ ] Tüm varsayılan şifreleri değiştirin
- [ ] JWT secret key'i güçlü bir değer ile değiştirin
- [ ] Encryption key'i 32 byte uzunluğunda ayarlayın
- [ ] SSL sertifikalarını yapılandırın
- [ ] Firewall kurallarını ayarlayın
- [ ] Regular backup'ları etkinleştirin

## 📁 Proje Yapısı

```
clustereye-test/
├── docker-compose.yml          # Ana servis tanımları
├── .env                        # Environment değişkenleri
├── deploy.sh                   # Otomatik kurulum scripti
├── docker/                     # Docker konfigürasyonları
│   ├── api/Dockerfile          # API container'ı
│   ├── nginx/                  # Nginx konfigürasyonu
│   ├── postgres/               # PostgreSQL init scripts
│   ├── frontend/               # Web arayüzü
│   └── grafana/                # Grafana dashboards
├── logs/                       # Log dosyaları
└── backups/                    # Veritabanı yedekleri
```

## 🛠️ Yönetim Komutları

### Temel Komutlar

```bash
# Servisleri başlat
docker-compose up -d

# Servisleri durdur
docker-compose down

# Servisleri yeniden başlat
docker-compose restart

# Log'ları görüntüle
docker-compose logs -f

# Belirli bir servisin log'larını görüntüle
docker-compose logs -f api
```

### Veritabanı Yönetimi

```bash
# PostgreSQL'e bağlan
docker exec -it clustereye-postgres psql -U clustereye_user -d clustereye

# Veritabanı backup'ı al
docker exec clustereye-postgres pg_dump -U clustereye_user clustereye > backup.sql

# Backup'ı geri yükle
docker exec -i clustereye-postgres psql -U clustereye_user clustereye < backup.sql
```

### InfluxDB Yönetimi

```bash
# InfluxDB CLI'ya erişim
docker exec -it clustereye-influxdb influx

# Bucket'ları listele
docker exec clustereye-influxdb influx bucket list
```

## 📊 İzleme ve Dashboard

### Grafana Dashboard'u

1. http://localhost:3000 adresine gidin
2. Admin kullanıcısı ile giriş yapın (şifre deployment sırasında gösterilir)
3. InfluxDB datasource otomatik olarak yapılandırılmıştır
4. Custom dashboard'lar ekleyebilirsiniz

### API Health Check

```bash
# API durumunu kontrol et
curl http://localhost/api/v1/health

# Agent listesini görüntüle
curl http://localhost/api/v1/agents
```

## 🔄 Güncelleme

```bash
# En son kodu çek
git pull

# Servisları güncelle
docker-compose build --no-cache
docker-compose up -d
```

## 🗑️ Temizlik

```bash
# Servisleri durdur ve container'ları sil
docker-compose down

# Volume'ları da sil (VERİ KAYBETMEYİN!)
docker-compose down -v

# Docker image'larını temizle
docker system prune -a
```

## 🆘 Sorun Giderme

### Yaygın Sorunlar

1. **Port çakışması**: Başka servisler 80, 443, 5432, 8086 portlarını kullanıyor olabilir
   - `.env` dosyasında port numaralarını değiştirin

2. **Yetersiz disk alanı**: Docker volume'ları çok yer kaplıyor olabilir
   - `docker system df` ile kullanımı kontrol edin
   - `docker system prune` ile temizlik yapın

3. **Servis başlamıyor**: Log'ları kontrol edin
   ```bash
   docker-compose logs servicename
   ```

4. **Veritabanı bağlantı hatası**: PostgreSQL'in hazır olmasını bekleyin
   ```bash
   docker-compose logs postgres
   ```

### Log Dosyaları

- **API Logs**: `./logs/`
- **Nginx Logs**: `./logs/nginx/`
- **PostgreSQL Logs**: `docker-compose logs postgres`
- **InfluxDB Logs**: `docker-compose logs influxdb`

## 📞 Destek

- 📚 **Dokümantasyon**: [GitHub README](https://github.com/sefaphlvn/clustereye-test)
- 🐛 **Bug Report**: GitHub Issues
- 💬 **Topluluk**: GitHub Discussions

## 📝 Notlar

- Bu kurulum production kullanıma hazırdır ancak güvenlik ayarları gözden geçirilmelidir
- Regular backup'lar alınması önerilir
- SSL sertifikası kullanımı zorunludur (production için)
- Sistem kaynaklarını izleyin ve gerektiğinde scaling yapın