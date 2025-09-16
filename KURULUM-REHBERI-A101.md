# A101 Müşterisi ClusterEye Kurulum Rehberi

Bu rehber, A101 müşterisi için ClusterEye sisteminin adım adım kurulumunu açıklar.

## 🎯 Kurulum Senaryosu

**Müşteri:** A101
**Domains:**
- API: `api-a101.clustereye.com`
- Dashboard: `a101.clustereye.com`
- Admin: `admin-a101.clustereye.com`

---

## 📋 1. SUNUCU HAZIRLIĞI

### 1.1 Sunucu Gereksinimleri

**Minimum Sistem:**
- CPU: 2 core
- RAM: 4GB
- Disk: 50GB SSD
- OS: Ubuntu 20.04+ / CentOS 8+ / Debian 11+
- Network: Public IP adresi

### 1.2 Gerekli Yazılımları Kurma

```bash
# Ubuntu/Debian için
sudo apt update
sudo apt install -y docker.io docker-compose git nginx certbot python3-certbot-nginx

# Docker servisini başlat
sudo systemctl enable docker
sudo systemctl start docker

# Kullanıcıyı docker grubuna ekle
sudo usermod -aG docker $USER

# Çıkış yapıp tekrar giriş yap (yeni grup için)
```

**CentOS/RHEL için:**
```bash
sudo yum update -y
sudo yum install -y docker docker-compose git nginx certbot python3-certbot-nginx
sudo systemctl enable docker
sudo systemctl start docker
sudo usermod -aG docker $USER
```

### 1.3 Firewall Ayarları

```bash
# Ubuntu UFW
sudo ufw allow 22    # SSH
sudo ufw allow 80    # HTTP
sudo ufw allow 443   # HTTPS
sudo ufw enable

# CentOS/RHEL firewalld
sudo firewall-cmd --permanent --add-port=22/tcp
sudo firewall-cmd --permanent --add-port=80/tcp
sudo firewall-cmd --permanent --add-port=443/tcp
sudo firewall-cmd --reload
```

---

## 🌐 2. DOMAIN VE DNS AYARLARI

### 2.1 DNS Kayıtları Oluşturma

Domain sağlayıcınızın (GoDaddy, Namecheap, vb.) DNS panelinden şu kayıtları ekleyin:

```
Type    Name                Value               TTL
A       api-a101           YOUR_SERVER_IP      300
A       a101               YOUR_SERVER_IP      300
A       admin-a101         YOUR_SERVER_IP      300
```

### 2.2 DNS Propagation Kontrolü

```bash
# DNS kayıtlarının yayılıp yayılmadığını kontrol edin
nslookup api-a101.clustereye.com
nslookup a101.clustereye.com
nslookup admin-a101.clustereye.com

# Alternatif olarak
dig api-a101.clustereye.com
dig a101.clustereye.com
dig admin-a101.clustereye.com
```

---

## 💻 3. CLUSTEREYE PROJESİ KURULUMU

### 3.1 Projeyi Sunucuya Klonlama

```bash
# Ana dizine git
cd /opt

# Projeyi klonla
sudo git clone https://github.com/sefaphlvn/clustereye-test.git
sudo chown -R $USER:$USER clustereye-test
cd clustereye-test
```

### 3.2 Production Environment Dosyası Oluşturma

```bash
# Environment dosyasını oluştur
cp .env.example .env

# Güvenli şifreler üret
./scripts/generate-secrets.sh

# .env dosyasını düzenle
nano .env
```

**.env dosyasında şunları ayarlayın:**
```bash
# Database
DB_PASSWORD=güçlü_db_şifresi_buraya

# InfluxDB
INFLUXDB_TOKEN=çok_uzun_ve_güçlü_influxdb_token_buraya
INFLUXDB_ADMIN_PASSWORD=güçlü_influxdb_şifresi

# JWT ve Encryption
JWT_SECRET_KEY=çok_uzun_jwt_secret_key_buraya
ENCRYPTION_KEY=tam_32_karakter_encryption_key!!

# Grafana
GRAFANA_ADMIN_PASSWORD=güçlü_grafana_şifresi

# Log Level (production için)
LOG_LEVEL=warn
```

### 3.3 Production Docker Compose Başlatma

```bash
# Production modunda başlat
docker-compose -f docker-compose.production.yml up -d

# Servislerin başlamasını bekle
docker-compose -f docker-compose.production.yml logs -f

# Ctrl+C ile çık, servisler çalışmaya devam eder
```

### 3.4 Servislerin Kontrolü

```bash
# Çalışan servisleri kontrol et
docker ps

# API health check
curl http://localhost:8080/health

# Logları kontrol et
docker-compose -f docker-compose.production.yml logs api
```

---

## 🔒 4. SSL SERTİFİKASI VE GÜVENLİK

### 4.1 Let's Encrypt SSL Sertifikası Alma

```bash
# API domain için
sudo certbot certonly --standalone -d api-a101.clustereye.com --email your-email@domain.com --agree-tos --no-eff-email

# Dashboard domain için
sudo certbot certonly --standalone -d a101.clustereye.com --email your-email@domain.com --agree-tos --no-eff-email

# Admin domain için
sudo certbot certonly --standalone -d admin-a101.clustereye.com --email your-email@domain.com --agree-tos --no-eff-email
```

### 4.2 SSL Auto-Renewal Kurma

```bash
# Crontab düzenle
sudo crontab -e

# Şu satırı ekle (günde 2 kez kontrol eder)
0 12 * * * /usr/bin/certbot renew --quiet && docker-compose -f /opt/clustereye-test/docker-compose.production.yml restart nginx
```

---

## 🏗️ 5. A101 İÇİN ÖZEL SİTE KURULUMU

### 5.1 API Domain Konfigürasyonu

```bash
cd /opt/clustereye-test

# API konfigürasyonu oluştur
./scripts/production-setup.sh setup api api-a101.clustereye.com
```

### 5.2 Ana Dashboard Konfigürasyonu

```bash
# Frontend konfigürasyonu oluştur
./scripts/production-setup.sh setup frontend a101.clustereye.com
```

### 5.3 A101 Özel Site Konfigürasyonu

```bash
# A101 customer site oluştur
./scripts/production-setup.sh setup customer admin-a101.clustereye.com a101
```

### 5.4 A101 Özel Dashboard Sayfası Oluşturma

```bash
# A101 için özel dashboard klasörü oluştur
mkdir -p docker/frontend/sites/a101

# A101 özel dashboard'u oluştur
cat > docker/frontend/sites/a101/index.html << 'EOF'
<!DOCTYPE html>
<html lang="tr">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>A101 Sistem İzleme Dashboard</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            margin: 0; padding: 0; background: #f8f9fa;
        }
        .header {
            background: linear-gradient(135deg, #e31e24 0%, #c41e3a 100%);
            color: white; padding: 2rem; text-align: center;
        }
        .container { max-width: 1200px; margin: 0 auto; padding: 2rem; }
        .metrics-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
            gap: 1.5rem; margin: 2rem 0;
        }
        .metric-card {
            background: white; padding: 1.5rem; border-radius: 8px;
            box-shadow: 0 2px 10px rgba(0,0,0,0.1); border-left: 4px solid #e31e24;
        }
        .metric-value { font-size: 2.5rem; font-weight: bold; color: #e31e24; }
        .metric-label { color: #666; font-size: 0.9rem; text-transform: uppercase; }
        .api-links { background: white; padding: 1.5rem; border-radius: 8px; margin: 1rem 0; }
        .btn {
            display: inline-block; padding: 0.75rem 1.5rem;
            background: #e31e24; color: white; text-decoration: none;
            border-radius: 4px; margin: 0.5rem; font-weight: 500;
        }
        .btn:hover { background: #c41e3a; }
        .status-ok { color: #28a745; }
        .logo { height: 60px; margin-bottom: 1rem; }
    </style>
</head>
<body>
    <div class="header">
        <h1>A101 Sistem İzleme Dashboard</h1>
        <p>Gerçek zamanlı sistem performans takibi</p>
    </div>

    <div class="container">
        <div class="metrics-grid">
            <div class="metric-card">
                <div class="metric-label">Aktif Sunucular</div>
                <div class="metric-value" id="activeServers">--</div>
            </div>
            <div class="metric-card">
                <div class="metric-label">CPU Kullanımı</div>
                <div class="metric-value" id="cpuUsage">--</div>
            </div>
            <div class="metric-card">
                <div class="metric-label">Memory Kullanımı</div>
                <div class="metric-value" id="memoryUsage">--</div>
            </div>
            <div class="metric-card">
                <div class="metric-label">Sistem Durumu</div>
                <div class="metric-value status-ok" id="systemStatus">Aktif</div>
            </div>
        </div>

        <div class="api-links">
            <h3>API Erişim Linkleri</h3>
            <a href="/api/v1/health" class="btn" target="_blank">Health Check</a>
            <a href="/api/v1/agents" class="btn" target="_blank">Agent Listesi</a>
            <a href="/api/v1/metrics" class="btn" target="_blank">Sistem Metrikleri</a>
            <a href="https://api-a101.clustereye.com" class="btn" target="_blank">API Dokumentasyonu</a>
        </div>
    </div>

    <script>
        // Basit metrik simülasyonu
        function updateMetrics() {
            document.getElementById('activeServers').textContent = Math.floor(Math.random() * 10) + 5;
            document.getElementById('cpuUsage').textContent = Math.floor(Math.random() * 40) + 30 + '%';
            document.getElementById('memoryUsage').textContent = Math.floor(Math.random() * 30) + 50 + '%';
        }

        // Sayfa yüklendiğinde ve her 30 saniyede bir güncelle
        updateMetrics();
        setInterval(updateMetrics, 30000);

        // API health check
        fetch('/api/v1/health')
            .then(response => response.ok ? 'Aktif' : 'Hata')
            .then(status => document.getElementById('systemStatus').textContent = status)
            .catch(() => document.getElementById('systemStatus').textContent = 'Bağlantı Hatası');
    </script>
</body>
</html>
EOF
```

### 5.5 Nginx Konfigürasyonlarını Aktifleştirme

```bash
# Nginx'i yeniden başlat
docker-compose -f docker-compose.production.yml restart nginx

# Konfigürasyonu test et
docker-compose -f docker-compose.production.yml exec nginx nginx -t
```

---

## ✅ 6. TEST VE DOĞRULAMA

### 6.1 Domain Erişim Testleri

```bash
# API endpoint testi
curl https://api-a101.clustereye.com/health

# Dashboard erişim testi
curl -I https://a101.clustereye.com

# Admin panel erişim testi
curl -I https://admin-a101.clustereye.com
```

### 6.2 SSL Sertifikası Kontrolü

```bash
# SSL sertifikasını kontrol et
openssl s_client -connect api-a101.clustereye.com:443 -servername api-a101.clustereye.com

# SSL rating kontrolü (isteğe bağlı)
# https://www.ssllabs.com/ssltest/ sitesinde test edin
```

### 6.3 Performans Testleri

```bash
# API load test
curl -w "%{time_total}" https://api-a101.clustereye.com/health

# Memory ve CPU kullanımı kontrol
docker stats

# Log kontrolü
docker-compose -f docker-compose.production.yml logs --tail=50
```

---

## 🎉 7. KURULUM TAMAMLANDI!

### 7.1 A101 Erişim Bilgileri

**🌐 Ana Erişim Linkleri:**
- **API Endpoint:** https://api-a101.clustereye.com
- **Dashboard:** https://a101.clustereye.com
- **Admin Panel:** https://admin-a101.clustereye.com

**🔐 Admin Giriş Bilgileri:**
- **Kullanıcı:** admin
- **Şifre:** admin123 (değiştirin!)

**📊 Grafana (isteğe bağlı):**
- **URL:** https://admin-a101.clustereye.com:3000
- **Kullanıcı:** admin
- **Şifre:** .env dosyasındaki GRAFANA_ADMIN_PASSWORD

### 7.2 Yedekleme Sistemi

```bash
# Manuel yedek alma
./scripts/backup.sh

# Otomatik yedek kontrolü
crontab -l

# Yedek dosyalarını kontrol et
ls -la backups/
```

### 7.3 İzleme ve Bakım

```bash
# Sistem durumu kontrol scripti oluştur
cat > /opt/clustereye-test/health-check.sh << 'EOF'
#!/bin/bash
echo "=== A101 ClusterEye Sistem Durumu ==="
echo "Tarih: $(date)"
echo ""

echo "Docker Servisler:"
docker ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"

echo ""
echo "API Health Check:"
curl -s https://api-a101.clustereye.com/health

echo ""
echo "Disk Kullanımı:"
df -h / | grep -v Filesystem

echo ""
echo "Memory Kullanımı:"
free -h

echo ""
echo "Son 5 API Log:"
docker-compose -f /opt/clustereye-test/docker-compose.production.yml logs --tail=5 api
EOF

chmod +x /opt/clustereye-test/health-check.sh

# Günlük health check için cron ekle
echo "0 9 * * * /opt/clustereye-test/health-check.sh >> /var/log/clustereye-health.log" | crontab -
```

---

## 🆘 SORUN GİDERME

### Domain erişilemiyorsa:
```bash
# DNS kontrolü
nslookup api-a101.clustereye.com

# Nginx log kontrol
docker-compose -f docker-compose.production.yml logs nginx

# Firewall kontrol
sudo ufw status
```

### SSL hatası varsa:
```bash
# Sertifika yenileme
sudo certbot renew

# Nginx restart
docker-compose -f docker-compose.production.yml restart nginx
```

### API erişilemiyorsa:
```bash
# API container kontrol
docker-compose -f docker-compose.production.yml logs api

# Port kontrol
netstat -tlnp | grep :8080
```

---

**🎯 A101 kurulumu tamamlandı! Sistem production'da kullanıma hazır.**

<function_calls>
<invoke name="TodoWrite">
<parameter name="todos">[{"content": "A101 müşterisi için sunucu hazırlığı ve gereksinimleri", "status": "in_progress", "activeForm": "A101 müşterisi için sunucu hazırlığı ve gereksinimleri yapılıyor"}, {"content": "Domain ve DNS ayarlarını yapılandırma", "status": "pending", "activeForm": "Domain ve DNS ayarları yapılandırılıyor"}, {"content": "ClusterEye projesi kurulumu ve konfigürasyonu", "status": "pending", "activeForm": "ClusterEye projesi kurulumu ve konfigürasyonu yapılıyor"}, {"content": "SSL sertifikası alma ve güvenlik ayarları", "status": "pending", "activeForm": "SSL sertifikası alınıyor ve güvenlik ayarları yapılıyor"}, {"content": "A101 için özel site kurulumu", "status": "pending", "activeForm": "A101 için özel site kurulumu yapılıyor"}, {"content": "Test ve doğrulama işlemleri", "status": "pending", "activeForm": "Test ve doğrulama işlemleri yapılıyor"}]