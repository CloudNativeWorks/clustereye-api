# AWS RDS SQL Server Monitoring - Kullanım Örnekleri

Bu doküman, ClusterEye projesine entegre edilen AWS RDS SQL Server monitoring özelliklerinin kullanım örneklerini içerir.

## API Endpoints

### 1. AWS Kimlik Bilgilerini Test Et

**Endpoint:** `POST /api/v1/aws/test-credentials`

**Açıklama:** Müşterinin AWS kimlik bilgilerini doğrular.

**İstek Örneği:**
```bash
curl -X POST http://localhost:8080/api/v1/aws/test-credentials \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -d '{
    "accessKeyId": "AKIAIOSFODNN7EXAMPLE",
    "secretAccessKey": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
    "region": "us-east-1"
  }'
```

**Başarılı Yanıt:**
```json
{
  "success": true,
  "message": "AWS credentials are valid"
}
```

**Hata Yanıtı:**
```json
{
  "success": false,
  "error": "Invalid AWS credentials: The security token included in the request is invalid"
}
```

### 2. RDS SQL Server Instance'larını Listele

**Endpoint:** `POST /api/v1/aws/rds/instances`

**Açıklama:** Belirtilen AWS bölgesindeki SQL Server RDS instance'larını getirir.

**İstek Örneği:**
```bash
curl -X POST http://localhost:8080/api/v1/aws/rds/instances \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -d '{
    "region": "us-east-1",
    "credentials": {
      "accessKeyId": "AKIAIOSFODNN7EXAMPLE",
      "secretAccessKey": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
      "region": "us-east-1"
    }
  }'
```

**Başarılı Yanıt:**
```json
{
  "status": "success",
  "instances": [
    {
      "DBInstanceIdentifier": "my-sql-server",
      "DBInstanceClass": "db.t3.large",
      "Engine": "sqlserver-se",
      "EngineVersion": "15.00.4073.23.v1",
      "DBInstanceStatus": "available",
      "Endpoint": {
        "Address": "my-sql-server.xxxxx.us-east-1.rds.amazonaws.com",
        "Port": 1433
      },
      "AllocatedStorage": 100,
      "StorageType": "gp2",
      "MultiAZ": false,
      "AvailabilityZone": "us-east-1a",
      "VpcSecurityGroups": [
        {
          "VpcSecurityGroupId": "sg-12345678",
          "Status": "active"
        }
      ],
      "DBSubnetGroup": {
        "DBSubnetGroupName": "default-vpc-12345678",
        "DBSubnetGroupDescription": "default DB subnet group",
        "VpcId": "vpc-12345678"
      }
    }
  ]
}
```

### 3. RDS SQL Server Bilgilerini Kaydet

**Endpoint:** `POST /api/v1/aws/rds/save`

**Açıklama:** AWS RDS SQL Server instance bilgilerini veritabanına kaydeder.

**İstek Örneği:**
```bash
curl -X POST http://localhost:8080/api/v1/aws/rds/save \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -d '{
    "clusterName": "production-cluster",
    "region": "us-east-1",
    "awsAccountId": "123456789012",
    "instanceInfo": {
      "DBInstanceIdentifier": "my-sql-server",
      "DBInstanceClass": "db.t3.large",
      "Engine": "sqlserver-se",
      "EngineVersion": "15.00.4073.23.v1",
      "DBInstanceStatus": "available",
      "MasterUsername": "admin",
      "AllocatedStorage": 100,
      "StorageType": "gp2",
      "MultiAZ": false,
      "AvailabilityZone": "us-east-1a",
      "PubliclyAccessible": false,
      "StorageEncrypted": true,
      "Endpoint": {
        "Address": "my-sql-server.xxxxx.us-east-1.rds.amazonaws.com",
        "Port": 1433
      }
    },
    "credentials": {
      "accessKeyId": "AKIAIOSFODNN7EXAMPLE",
      "secretAccessKey": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
      "region": "us-east-1"
    }
  }'
```

**Başarılı Yanıt:**
```json
{
  "status": "success",
  "message": "RDS MSSQL information saved successfully"
}
```

**Hata Yanıtı:**
```json
{
  "status": "error",
  "error": "Failed to save RDS info to database: connection timeout"
}
```

### 4. Kayıtlı RDS Instance'larını Getir

**Endpoint:** `GET /api/v1/aws/rds/instances`

**Açıklama:** Veritabanında kayıtlı RDS instance'larını getirir (şifreli bilgiler çözülür).

**İstek Örneği:**
```bash
curl -X GET http://localhost:8080/api/v1/aws/rds/instances \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

**Başarılı Yanıt:**
```json
{
  "success": true,
  "data": [
    {
      "id": 1,
      "clustername": "production-cluster",
      "region": "us-east-1",
      "aws_account_id": "123456789012",
      "jsondata": {
        "DBInstanceIdentifier": "my-sql-server",
        "DBInstanceClass": "db.t3.large",
        "Engine": "sqlserver-se",
        "awsCredentials": {
          "accessKeyId": "AKIA...",
          "secretAccessKey": "decrypted-secret-key",
          "region": "us-east-1"
        },
        "sqlCredentials": {
          "username": "admin",
          "password": "decrypted-password"
        }
      },
      "created_at": "2024-01-15T10:00:00Z",
      "updated_at": "2024-01-15T10:00:00Z"
    }
  ]
}
```

### 5. SQL Server Bağlantısını Test Et

**Endpoint:** `POST /api/v1/aws/rds/test-connection`

**Açıklama:** SQL Server bağlantısını test eder.

**İstek Örneği:**
```bash
curl -X POST http://localhost:8080/api/v1/aws/rds/test-connection \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -d '{
    "endpoint": "my-sql-server.xxxxx.us-east-1.rds.amazonaws.com",
    "port": 1433,
    "username": "admin",
    "password": "your-password",
    "database": "master"
  }'
```

**Başarılı Yanıt:**
```json
{
  "success": true,
  "message": "Connection successful"
}
```

**Hata Yanıtı:**
```json
{
  "success": false,
  "error": "Connection test failed: Login failed for user 'admin'"
}
```

### 6. CloudWatch Metriklerini Getir

**Endpoint:** `POST /api/v1/aws/cloudwatch/metrics`

**Açıklama:** RDS instance için CloudWatch metriklerini getirir.

**İstek Örneği:**
```bash
curl -X POST http://localhost:8080/api/v1/aws/cloudwatch/metrics \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -d '{
    "instanceId": "my-sql-server",
    "metricName": "CPUUtilization",
    "region": "us-east-1",
    "range": "1h",
    "credentials": {
      "accessKeyId": "AKIAIOSFODNN7EXAMPLE",
      "secretAccessKey": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
      "region": "us-east-1"
    }
  }'
```

**Başarılı Yanıt:**
```json
{
  "status": "success",
  "data": [
    {
      "timestamp": "2024-01-15T10:00:00Z",
      "value": 45.5,
      "unit": "Percent"
    },
    {
      "timestamp": "2024-01-15T10:05:00Z",
      "value": 42.1,
      "unit": "Percent"
    },
    {
      "timestamp": "2024-01-15T10:10:00Z",
      "value": 38.7,
      "unit": "Percent"
    }
  ]
}
```

## Desteklenen CloudWatch Metrikleri

### CPU ve Memory
- `CPUUtilization` - CPU kullanım yüzdesi
- `CPUCreditUsage` - CPU kredisi kullanımı (burstable instance'lar için)
- `CPUCreditBalance` - CPU kredisi bakiyesi

### Database Connections
- `DatabaseConnections` - Aktif veritabanı bağlantı sayısı

### Storage
- `FreeStorageSpace` - Boş depolama alanı (bytes)
- `FreeableMemory` - Boş RAM (bytes)

### Network
- `NetworkReceiveThroughput` - Ağ giriş trafiği (bytes/saniye)
- `NetworkTransmitThroughput` - Ağ çıkış trafiği (bytes/saniye)

### Database Performance
- `ReadLatency` - Okuma gecikmesi (saniye)
- `WriteLatency` - Yazma gecikmesi (saniye)
- `ReadThroughput` - Okuma verimi (bytes/saniye)
- `WriteThroughput` - Yazma verimi (bytes/saniye)
- `ReadIOPS` - Saniye başına okuma işlemi
- `WriteIOPS` - Saniye başına yazma işlemi

## Zaman Aralıkları

API'de desteklenen zaman aralıkları:
- `10m` - Son 10 dakika
- `30m` - Son 30 dakika
- `1h` - Son 1 saat (varsayılan)
- `6h` - Son 6 saat
- `24h` - Son 24 saat
- `7d` - Son 7 gün

## Hata Kodları

### AWS Kimlik Doğrulama Hataları
- `AWS_CREDENTIALS_INVALID` - Geçersiz AWS kimlik bilgileri
- `AWS_REGION_NOT_FOUND` - Geçersiz AWS bölgesi
- `AWS_ACCESS_DENIED` - Erişim reddedildi

### Rate Limiting
- `AWS_RATE_LIMIT_EXCEEDED` - Rate limit aşıldı (dakikada 10 istek)

### Genel Hatalar
- `REQUEST_TOO_LARGE` - İstek çok büyük (max 1MB)
- `INVALID_CONTENT_TYPE` - Geçersiz content type (application/json gerekli)

## JavaScript/TypeScript Örneği

```typescript
interface AWSCredentials {
  accessKeyId: string;
  secretAccessKey: string;
  region: string;
  sessionToken?: string;
}

interface RDSInstance {
  DBInstanceIdentifier: string;
  DBInstanceClass: string;
  Engine: string;
  EngineVersion: string;
  DBInstanceStatus: string;
  Endpoint: {
    Address: string;
    Port: number;
  };
}

class AWSRDSMonitoring {
  private baseUrl: string;
  private token: string;

  constructor(baseUrl: string, token: string) {
    this.baseUrl = baseUrl;
    this.token = token;
  }

  async testCredentials(credentials: AWSCredentials): Promise<boolean> {
    const response = await fetch(`${this.baseUrl}/api/v1/aws/test-credentials`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${this.token}`
      },
      body: JSON.stringify(credentials)
    });

    const data = await response.json();
    return data.success;
  }

  async getRDSInstances(region: string, credentials: AWSCredentials): Promise<RDSInstance[]> {
    const response = await fetch(`${this.baseUrl}/api/v1/aws/rds/instances`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${this.token}`
      },
      body: JSON.stringify({ region, credentials })
    });

    const data = await response.json();
    return data.instances || [];
  }

  async getSavedRDSInstances(): Promise<any[]> {
    const response = await fetch(`${this.baseUrl}/api/v1/aws/rds/instances`, {
      method: 'GET',
      headers: {
        'Authorization': `Bearer ${this.token}`
      }
    });

    const data = await response.json();
    return data.success ? data.data : [];
  }

  async saveRDSInfo(clusterName: string, instanceInfo: RDSInstance, credentials: AWSCredentials, awsAccountId?: string): Promise<boolean> {
    const response = await fetch(`${this.baseUrl}/api/v1/aws/rds/save`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${this.token}`
      },
      body: JSON.stringify({
        clusterName,
        region: credentials.region,
        instanceInfo,
        credentials,
        awsAccountId
      })
    });

    const data = await response.json();
    return data.status === 'success';
  }

  async testSQLConnection(endpoint: string, port: number, username: string, password: string, database: string): Promise<boolean> {
    const response = await fetch(`${this.baseUrl}/api/v1/aws/rds/test-connection`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${this.token}`
      },
      body: JSON.stringify({
        endpoint,
        port,
        username,
        password,
        database
      })
    });

    const data = await response.json();
    return data.success;
  }

  async getMetrics(instanceId: string, metricName: string, range: string, credentials: AWSCredentials) {
    const response = await fetch(`${this.baseUrl}/api/v1/aws/cloudwatch/metrics`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${this.token}`
      },
      body: JSON.stringify({
        instanceId,
        metricName,
        region: credentials.region,
        range,
        credentials
      })
    });

    const data = await response.json();
    return data.data || [];
  }
}

// Kullanım örneği
const monitoring = new AWSRDSMonitoring('http://localhost:8080', 'your-jwt-token');

const credentials = {
  accessKeyId: 'AKIA...',
  secretAccessKey: '...',
  region: 'us-east-1'
};

// Kimlik bilgilerini test et
const isValid = await monitoring.testCredentials(credentials);
if (isValid) {
  // RDS instance'larını getir
  const instances = await monitoring.getRDSInstances('us-east-1', credentials);
  
  if (instances.length > 0) {
    const firstInstance = instances[0];
    
    // RDS bilgilerini veritabanına kaydet
    const saved = await monitoring.saveRDSInfo(
      'production-cluster',
      firstInstance,
      credentials,
      '123456789012'
    );
    
    if (saved) {
      console.log('RDS bilgileri başarıyla kaydedildi');
      
      // CPU metriklerini getir
      const cpuMetrics = await monitoring.getMetrics(
        firstInstance.DBInstanceIdentifier,
        'CPUUtilization',
        '1h',
        credentials
      );
      console.log('CPU Metrics:', cpuMetrics);
    }
  }
}
```

## Python Örneği

```python
import requests
import json
from typing import Dict, List, Optional

class AWSRDSMonitoring:
    def __init__(self, base_url: str, token: str):
        self.base_url = base_url
        self.token = token
        self.headers = {
            'Content-Type': 'application/json',
            'Authorization': f'Bearer {token}'
        }
    
    def test_credentials(self, credentials: Dict) -> bool:
        """AWS kimlik bilgilerini test et"""
        response = requests.post(
            f'{self.base_url}/api/v1/aws/test-credentials',
            headers=self.headers,
            json=credentials
        )
        return response.json().get('success', False)
    
    def get_rds_instances(self, region: str, credentials: Dict) -> List[Dict]:
        """RDS instance'larını getir"""
        payload = {
            'region': region,
            'credentials': credentials
        }
        response = requests.post(
            f'{self.base_url}/api/v1/aws/rds/instances',
            headers=self.headers,
            json=payload
        )
        data = response.json()
        return data.get('instances', [])
    
    def save_rds_info(self, cluster_name: str, instance_info: Dict,
                     credentials: Dict, aws_account_id: str = None) -> bool:
        """RDS bilgilerini veritabanına kaydet"""
        payload = {
            'clusterName': cluster_name,
            'region': credentials['region'],
            'instanceInfo': instance_info,
            'credentials': credentials
        }
        if aws_account_id:
            payload['awsAccountId'] = aws_account_id
            
        response = requests.post(
            f'{self.base_url}/api/v1/aws/rds/save',
            headers=self.headers,
            json=payload
        )
        data = response.json()
        return data.get('status') == 'success'

    def get_metrics(self, instance_id: str, metric_name: str, 
                   time_range: str, credentials: Dict) -> List[Dict]:
        """CloudWatch metriklerini getir"""
        payload = {
            'instanceId': instance_id,
            'metricName': metric_name,
            'region': credentials['region'],
            'range': time_range,
            'credentials': credentials
        }
        response = requests.post(
            f'{self.base_url}/api/v1/aws/cloudwatch/metrics',
            headers=self.headers,
            json=payload
        )
        data = response.json()
        return data.get('data', [])

# Kullanım örneği
monitoring = AWSRDSMonitoring('http://localhost:8080', 'your-jwt-token')

credentials = {
    'accessKeyId': 'AKIA...',
    'secretAccessKey': '...',
    'region': 'us-east-1'
}

# Kimlik bilgilerini test et
if monitoring.test_credentials(credentials):
    print("AWS kimlik bilgileri geçerli")
    
    # RDS instance'larını getir
    instances = monitoring.get_rds_instances('us-east-1', credentials)
    print(f"Bulunan SQL Server instance sayısı: {len(instances)}")
    
    if instances:
        first_instance = instances[0]
        instance_id = first_instance['DBInstanceIdentifier']
        
        # RDS bilgilerini veritabanına kaydet
        saved = monitoring.save_rds_info(
            'production-cluster', 
            first_instance, 
            credentials, 
            '123456789012'
        )
        
        if saved:
            print("RDS bilgileri başarıyla kaydedildi")
            
            # CPU metriklerini getir
            cpu_metrics = monitoring.get_metrics(
                instance_id, 'CPUUtilization', '1h', credentials
            )
            print(f"CPU metrikleri: {len(cpu_metrics)} veri noktası")
        else:
            print("RDS bilgileri kaydedilemedi")
else:
    print("AWS kimlik bilgileri geçersiz")
```

## Güvenlik Notları

1. **🔐 Credential Encryption**: AWS kimlik bilgileri ve SQL Server şifreleri AES-256-GCM ile şifrelenerek veritabanında saklanır.

2. **🔑 Encryption Key**: `ENCRYPTION_KEY` environment variable'ı tam olarak 32 byte olmalıdır. Üretimde mutlaka değiştirin!

3. **⚡ Rate Limiting**: AWS endpoint'leri dakikada 10 istek ile sınırlıdır.

4. **📏 Request Boyutu**: İstek boyutu maksimum 1MB ile sınırlıdır.

5. **🔒 HTTPS Kullanın**: Üretim ortamında mutlaka HTTPS kullanın.

6. **🛡️ IAM Permissions**: AWS kullanıcısının aşağıdaki izinlere sahip olması gerekir:
   - `rds:DescribeDBInstances`
   - `cloudwatch:GetMetricStatistics`
   - `sts:GetCallerIdentity`

7. **🗄️ Database Security**: Hassas bilgiler şifrelenerek saklanır:
   - AWS Secret Access Key
   - AWS Session Token
   - SQL Server Passwords

8. **🔄 Key Rotation**: Encryption key'i periyodik olarak rotate edin.

## Sorun Giderme

### Yaygın Hatalar

1. **"Invalid AWS credentials"**
   - AWS Access Key ID ve Secret Access Key'i kontrol edin
   - AWS hesabının aktif olduğundan emin olun

2. **"Rate limit exceeded"**
   - İstekler arasında bekleme süresi ekleyin
   - Dakikada en fazla 10 istek gönderebilirsiniz

3. **"Failed to describe DB instances"**
   - IAM kullanıcısının RDS okuma izinleri olduğunu kontrol edin
   - Bölge (region) bilgisinin doğru olduğundan emin olun

4. **"No data points returned"**
   - Instance ID'nin doğru olduğunu kontrol edin
   - Metrik adının doğru yazıldığından emin olun
   - Zaman aralığını değiştirmeyi deneyin

### Log Kontrolü

Sunucu loglarında AWS API çağrıları izlenebilir:

```bash
# Log dosyasını takip et
tail -f logs/clustereye.log | grep "AWS"
```

Bu entegrasyon sayesinde müşteriler kendi AWS RDS SQL Server instance'larını ClusterEye dashboard'unda izleyebilir ve detaylı performans metriklerine erişebilirler.