# 🚀 ClusterEye Otomatik Kurulum Sistemi

Müşterileriniz için tek komutla ClusterEye kurulumu yapın!

## ⚡ Hızlı Kurulum

### A101 Örneği - Tek Komut:

```bash
./quick-deploy.sh A101 api-a101.clustereye.com a101.clustereye.com admin@a101.com.tr
```

**🕐 Süre:** 10-15 dakika (tamamen otomatik)

### Diğer Müşteriler:

```bash
# Migros
./quick-deploy.sh Migros api-migros.clustereye.com migros.clustereye.com admin@migros.com.tr

# CarrefourSA
./quick-deploy.sh CarrefourSA api-carrefour.clustereye.com carrefour.clustereye.com admin@carrefoursa.com

# Teknosa
./quick-deploy.sh Teknosa api-teknosa.clustereye.com teknosa.clustereye.com admin@teknosa.com.tr
```

---

## 🎯 Script'in Yaptığı İşlemler

### ✅ Otomatik olarak:

1. **Sistem Hazırlığı** (2 dk)
   - Docker, Docker Compose, Nginx, Certbot kurulumu
   - Firewall konfigürasyonu
   - Kullanıcı izinleri

2. **DNS Kontrolü** (1 dk)
   - Domain yayılım kontrolü
   - A record doğrulaması

3. **Güvenlik Ayarları** (2 dk)
   - Güçlü şifreler üretimi (64+ karakter)
   - JWT secret, encryption key
   - Tüm service şifreleri

4. **Docker Deployment** (5 dk)
   - PostgreSQL + InfluxDB kurulumu
   - ClusterEye API build ve start
   - Health check'ler

5. **SSL Sertifikaları** (3 dk)
   - Let's Encrypt sertifika alma
   - Otomatik yenileme kurulumu
   - HTTPS redirect'leri

6. **Site Konfigürasyonları** (2 dk)
   - Nginx virtual host'ları
   - Customer özel dashboard
   - API proxy ayarları

7. **Test ve Doğrulama** (1 dk)
   - Endpoint testleri
   - SSL kontrolleri
   - Health check'ler

8. **Monitoring Kurulumu** (1 dk)
   - Otomatik backup sistemi
   - Health monitoring
   - Log rotation

---

## 📋 Gereksinimler

### Sunucu:
- **OS:** Ubuntu 20.04+ / CentOS 8+ / Debian 11+
- **CPU:** 2 core minimum
- **RAM:** 4GB minimum
- **Disk:** 50GB SSD
- **Network:** Public IP, sudo yetkisi

### DNS:
Domain sağlayıcınızda A record'ları ekleyin:
```
A    api-musteri     SUNUCU_IP
A    musteri         SUNUCU_IP
```

---

## 🎮 Kullanım Yöntemleri

### 1. Tek Komut (Önerilen):
```bash
./quick-deploy.sh CUSTOMER_NAME API_DOMAIN FRONTEND_DOMAIN EMAIL
```

### 2. İnteraktif Mod:
```bash
./quick-deploy.sh
# Adım adım bilgi girersiniz
```

### 3. A101 Hızlı Mod:
```bash
./quick-deploy.sh a101
# A101 için hazır ayarlarla kurulum
```

---

## 📊 Kurulum Sonrası

### 🌐 Erişim Linkleri:
- **API:** https://api-musteri.clustereye.com
- **Dashboard:** https://musteri.clustereye.com

### 🔐 Giriş Bilgileri:
Script sonunda `musteri-credentials.txt` dosyası oluşturulur:
- ClusterEye admin şifresi
- Database şifreleri
- API token'ları

### 🛠️ Yönetim Komutları:
```bash
# Servis durumu
docker ps

# Logları görüntüle
docker-compose -f docker-compose.production.yml logs -f

# Yeniden başlat
docker-compose -f docker-compose.production.yml restart

# Sistem health check
./health-check.sh

# Backup al
./scripts/backup.sh
```

---

## 🔧 Troubleshooting

### Script hata verirse:

```bash
# Log kontrol et
tail -f /var/log/clustereye-deployment.log

# DNS kontrol et
nslookup api-musteri.clustereye.com

# Docker kontrol et
docker ps -a

# Nginx kontrol et
docker-compose exec nginx nginx -t
```

### Yaygın Sorunlar:

1. **DNS henüz yayılmamış**
   - 15-30 dakika bekleyin
   - `nslookup domain.com` ile kontrol edin

2. **Port çakışması**
   - Başka web server kapatın: `sudo systemctl stop apache2`

3. **SSL hatası**
   - Domain'in sunucuya doğru yönlendirdiğinden emin olun

---

## 💡 Pro Tips

### Çoklu Müşteri Kurulumu:
```bash
# Paralel kurulum (farklı sunucularda)
./quick-deploy.sh A101 api-a101.clustereye.com a101.clustereye.com admin-a101.clustereye.com admin@a101.com.tr &
./quick-deploy.sh Migros api-migros.clustereye.com migros.clustereye.com admin-migros.clustereye.com admin@migros.com.tr &
```

### Backup Stratejisi:
```bash
# Günlük otomatik backup (script otomatik kurar)
0 2 * * * /opt/clustereye-test/scripts/backup.sh

# Manuel backup
./scripts/backup.sh
```

### Performance Monitoring:
```bash
# Resource kullanımı
docker stats

# API performance
curl -w "%{time_total}" https://api-musteri.clustereye.com/health
```

---

## 📞 Destek

- **Log Dosyası:** `/var/log/clustereye-deployment.log`
- **Proje Klasörü:** `/opt/clustereye-test`
- **Credentials:** `musteri-credentials.txt`

**🎉 Tek komutla production-ready ClusterEye sistemi!**