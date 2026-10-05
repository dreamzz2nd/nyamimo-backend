# 📖 Panduan Lengkap Instalasi & Deployment Nyamimo Anime Stream

Dokumen panduan ini dirancang untuk pembeli/pengembang agar dapat menjalankan dan mendeploy website **Nyamimo** dengan cepat di berbagai lingkungan (Windows, Linux VPS Ubuntu, Docker, dan Cloud Gratis).

---

## ⚡ 1. Cara Menjalankan di Komputer / Laptop (Windows)

Jika kamu ingin menjalankan atau menguji coba website di komputer Windows lokal:
1. Pastikan folder projek sudah diekstrak lengkap.
2. Klik ganda (double-click) file **`nyamimo-server.exe`** atau buka terminal / Command Prompt di folder projek dan ketik:
   ```cmd
   .\nyamimo-server.exe
   ```
3. Buka browser dan kunjungi: **`http://localhost:8080`** (atau port yang tertera di terminal).
4. Untuk login ke Dashboard Administrator:
   - URL: `http://localhost:8080/admin`
   - Username: `admin`
   - Password: `admin123`

---

## 🚀 2. Cara Deploy di Linux VPS (Ubuntu / Debian + Nginx + SSL Gratis)

Metode ini adalah metode paling direkomendasikan untuk situs produksi dengan domain sendiri (misal: `animeku.com`).

### Langkah 1: Persiapan Server VPS
Buka terminal SSH ke VPS kamu (misal DigitalOcean, Linode, AWS, Contabo, atau Biznet):
```bash
sudo apt update && sudo apt upgrade -y
sudo apt install -y git wget curl nginx certbot python3-certbot-nginx
```

### Langkah 2: Upload File & Jalankan Sebagai Systemd Service
1. Copy folder projek Nyamimo ke `/var/www/nyamimo`.
2. Berikan izin eksekusi pada binary Linux:
   ```bash
   chmod +x /var/www/nyamimo/nyamimo-server
   ```
   *(Atau jika ingin compile ulang di VPS: `go build -o nyamimo-server main.go`)*

3. Buat service otomatis agar aplikasi otomatis restart jika server reboot:
   ```bash
   sudo nano /etc/systemd/system/nyamimo.service
   ```
   Tempelkan konfigurasi berikut:
   ```ini
   [Unit]
   Description=Nyamimo Anime Stream Service
   After=network.target

   [Service]
   Type=simple
   User=root
   WorkingDirectory=/var/www/nyamimo
   ExecStart=/var/www/nyamimo/nyamimo-server
   Restart=always
   RestartSec=5
   Environment=PORT=8080

   [Install]
   WantedBy=multi-user.target
   ```
4. Aktifkan dan jalankan service:
   ```bash
   sudo systemctl daemon-reload
   sudo systemctl enable nyamimo
   sudo systemctl start nyamimo
   ```

### Langkah 3: Konfigurasi Nginx Reverse Proxy & SSL (Domain)
1. Buat konfigurasi virtual host Nginx:
   ```bash
   sudo nano /etc/nginx/sites-available/nyamimo
   ```
   Tempelkan konfigurasi berikut (ganti `domainkamu.com` dengan domain milikmu):
   ```nginx
   server {
       listen 80;
       server_name domainkamu.com www.domainkamu.com;

       location / {
           proxy_pass http://127.0.0.1:8080;
           proxy_http_version 1.1;
           proxy_set_header Upgrade $http_upgrade;
           proxy_set_header Connection 'upgrade';
           proxy_set_header Host $host;
           proxy_set_header X-Real-IP $remote_addr;
           proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
           proxy_set_header X-Forwarded-Proto $scheme;
           proxy_cache_bypass $http_upgrade;
       }
   }
   ```
2. Aktifkan situs di Nginx:
   ```bash
   sudo ln -s /etc/nginx/sites-available/nyamimo /etc/nginx/sites-enabled/
   sudo nginx -t
   sudo systemctl restart nginx
   ```
3. Pasang SSL HTTPS Gratis (Let's Encrypt):
   ```bash
   sudo certbot --nginx -d domainkamu.com -d www.domainkamu.com
   ```
   *Selesai! Website kamu kini aktif di https://domainkamu.com.*

---

## 🐳 3. Cara Deploy Menggunakan Docker (1-Command Run)

Jika kamu menyukai container Docker:
```bash
docker compose up -d --build
```
Aplikasi langsung berjalan di port `8080`.

---

## ☁️ 4. Cara Deploy Gratis di Cloud (Render / Railway / Koyeb)

1. Upload source code ke repositori GitHub pribadi/publik kamu.
2. Buat akun di **[Render.com](https://render.com)** atau **[Railway.app](https://railway.app)**.
3. Pilih **New Web Service** -> Hubungkan repositori GitHub Nyamimo.
4. Setting konfigurasi:
   - **Environment**: `Go` atau `Docker`
   - **Build Command**: `go build -o nyamimo-server main.go`
   - **Start Command**: `./nyamimo-server`
5. Klik **Deploy** -> Website langsung aktif dengan domain HTTPS gratis (contoh: `nyamimo.onrender.com`).

---

## 💰 5. Panduan Monetisasi Iklan (Ad Placement Manager)

Website ini sudah dilengkapi fitur pemasangan iklan dari **Admin Dashboard**:
1. Masuk ke halaman `/admin`.
2. Klik tab **Pengaturan Iklan (Monetisasi)**.
3. Tersedia 4 slot iklan strategis:
   - **Header Top Banner**: Banner 728x90 di bagian atas.
   - **Bawah Pemutar Video**: Banner 300x250 / 728x90 di bawah video player (CTR & impresi tertinggi).
   - **Popunder / Direct Script**: Script popup di head/body (penghasilan CPM terbesar di web anime).
   - **Footer Banner**: Banner sticky di bawah halaman.
4. Aktifkan toggle **ON**, tempel script iklan dari jaringan iklan (misal: *Adsterra, Monetag, PropellerAds, Adsense*), lalu klik **Simpan Semua Iklan**. Iklan langsung tayang tanpa perlu reload server!

---

## ⚙️ 6. Cara Mengganti Brand, Logo, & Endpoint API (`config.json`)

Kamu bisa mengganti identitas website dengan 2 cara:
1. **Lewat Admin Dashboard**:
   - Masuk ke `/admin` -> Tab **Konfigurasi Website & API**.
   - Ubah Nama Website, Slogan, URL Logo, dan Endpoint API, lalu klik **Simpan Konfigurasi**.
2. **Lewat file `config.json`**:
   - Buka file `config.json` di root direktori menggunakan teks editor (Notepad/VSCode), sesuaikan nilainya, dan simpan.
