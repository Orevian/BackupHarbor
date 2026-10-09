# BackupHarbor

**restic yedeklerinizin gerçekten geri yüklenebildiğini kanıtlayın.**

BackupHarbor, mevcut [restic](https://restic.net) yedeklerinizi özel bir çalışma
alanına geri yükleyen, geri yüklenen dosyaları denetleyen ve neyin kanıtlanıp neyin
kanıtlanamadığını açıkça raporlayan bağımsız bir Windows uygulamasıdır. Bir masaüstü
uygulamasından (`BackupHarbor.exe`) ve bir komut satırı aracından (`bharbor.exe`)
oluşur; ikisi de aynı doğrulama motorunu, aynı yapılandırma dosyasını ve aynı rapor
geçmişini kullanır.

> "Başarılı" görünen bir yedekleme, geri yükleyebileceğiniz bir yedek demek değildir.
> BackupHarbor başarıyı varsaymaz: yalnızca gerçekten çalışan ve geçen kontroller
> başarılı olarak raporlanır.

[English README](README.md)

## Sorun

Yedekleme araçları verinin *yazıldığını* bildirir; verinin geri okunabildiğini,
çözülebildiğini ve kullanılabildiğini söylemez. Sessiz depo hasarı, kaybolmuş bir
parola, eksik bir snapshot ya da yazma sırasında kopyalanmış bir veritabanı ancak en
kötü günde fark edilir. Geri yüklemeyi elle test etmek zahmetlidir, bu yüzden genellikle
atlanır.

## BackupHarbor ne yapar

Yapılandırdığınız her depo ("hedef") için gerçek restic komutları çalıştırır:

1. **Ön kontrol** - restic'i bulur ve sürümünü denetler, parola kaynağını kontrol eder, depoyu açar, snapshot'ı seçer, çalışma dizinini denetler.
2. **Bütünlük kontrolü** - `restic check`; isteğe bağlı olarak saklanan verinin tamamı ya da bir örneği okunur.
3. **Geri yükleme** - Snapshot, kendi oluşturduğu özel bir çalışma alanına `restic restore --verify` ile geri yüklenir (dosyalar geri okunup depodaki özetlerle karşılaştırılır). Süre, dosya sayısı ve boyut ölçülür.
4. **İçerik kuralları** - Tanımladığınız denetimler: dosya var mı, boş mu, okunabilir metin, geçerli JSON/XML, ZIP açılıyor mu (CRC dahil), PDF yapısı, SQLite `integrity_check`, SHA-256.
5. **Temizlik** - Çalışma alanı, yalnızca onu kendisinin oluşturduğu kanıtlandıktan sonra silinir.
6. **Rapor** - JSON, HTML ve TXT; sonucun neyi kanıtlayıp neyi kanıtlamadığı açıkça yazılır.

Her aşama `passed` (başarılı), `failed` (başarısız), `not_run` (çalıştırılamadı),
`cancelled` (iptal), `timed_out` (zaman aşımı), `partial` (kısmi) olarak biter;
devre dışı bıraktığınız aşamalar `skipped` olarak görünür. Test edilemeyen bir yedek
asla başarılı sayılmaz.

## Özellikler

- Genel bakış, canlı doğrulama ilerlemesi, hedef düzenleyici, rapor tarayıcısı, zamanlama ve ayarlar içeren masaüstü uygulaması; açık, koyu ve sistem teması; Türkçe ve İngilizce.
- Betikler ve sunucular için komut satırı aracı: `NO_COLOR` destekli renk, `--json` çıktısı, kararlı çıkış kodları.
- Uygulamanın yönettiği Windows Görev Zamanlayıcı entegrasyonu (yalnızca kendi görevine dokunulur).
- Parolalar yapılandırmada asla saklanmaz: Windows Kimlik Bilgisi Yöneticisi, ortam değişkeni veya parola dosyası.
- Gerçek depolara hiç dokunmayan, açıkça işaretli örnek verili demo modu.
- Telemetri yok, analitik yok, otomatik güncelleme yok, gizli indirme yok.

## Gereksinimler

- Windows 10 veya 11, x64.
- [restic](https://restic.net) 0.16 veya daha yeni. Kurulum programı ve taşınabilir ZIP, değiştirilmemiş bir restic'i (sürüm `third_party/restic/PIN` içinde) zaten içerir; ayrıca bir şey kurmanız gerekmez, isterseniz Ayarlar'dan başka bir restic seçebilirsiniz.
- Masaüstü uygulaması için Microsoft Edge WebView2 Çalışma Zamanı (güncel Windows 11'de ve çoğu Windows 10'da hazır gelir). Komut satırı aracı buna ihtiyaç duymaz.

## Kurulum

BackupHarbor şimdilik kaynaktan derlenir; henüz yayımlanmış bir sürüm yoktur.
Sürümler yayımlandığında kurulum programı (`BackupHarbor-<sürüm>-setup.exe`), taşınabilir
klasör (ZIP) ve `SHA256SUMS.txt` içerecektir. İkisi de `restic.exe`'yi uygulamanın yanında getirir; başka bir şey kurmadan çalışırlar (masaüstü uygulaması ayrıca WebView2 Çalışma Zamanı ister).

Kendiniz derleyin (Go 1.26+ ve MinGW `windres` gerekir; kurulum programı için Inno Setup 6 isteğe bağlı):

```powershell
powershell -ExecutionPolicy Bypass -File scripts\build.ps1
```

Betik kodu biçimlendirme, `go vet` ve testlerden geçirir, simge ve sürüm bilgisini
gömer, sabitlenmiş resmî restic sürümünü indirir (SHA-256 değeri `third_party/restic/PIN` ile doğrulanır; çevrimdışı derleme için `-ResticZip`), iki çalıştırılabilir dosyayı `dist\BackupHarbor-<sürüm>-win-x64\` klasörüne
(*onedir* düzeni; tek dosyalık kendi kendini açan paket değil) derler, ZIP oluşturur,
Inno Setup kuruluysa kurulum programını üretir ve SHA-256 sağlama toplamlarını yazar.
Ayrıntılar için [CONTRIBUTING.md](CONTRIBUTING.md).

Kurulum programı Program Files'a kurulum, Başlat menüsü kısayolu, isteğe bağlı masaüstü
kısayolu ve `bharbor` için isteğe bağlı PATH girdisi sunar. Kaldırma sırasında yalnızca
kurulum programının kendi eklediği PATH girdisi silinir; siz açıkça onaylamadıkça
yapılandırmanız ve raporlarınız korunur.

## Hızlı başlangıç

Komut satırı:

```powershell
bharbor init                      # örnek yapılandırma yazar (asla üzerine yazmaz)
bharbor secret set yedegim        # depo parolasını Windows Kimlik Bilgisi Yöneticisi'ne kaydeder
notepad $env:APPDATA\BackupHarbor\config.yaml
bharbor validate                  # geri yükleme dışında her şeyi denetler
bharbor run                       # etkin tüm hedefleri doğrular
bharbor report --last             # son raporu gösterir
```

Masaüstü uygulaması: `BackupHarbor`'u başlatın, genel bakış sayfasındaki kurulum
rehberini izleyin (restic, yapılandırma, ilk hedef) ve **Şimdi test et**'e basın.
Hiçbir yedeğiniz olmadan göz atmak için **Demo modu**'nu kullanın.

## Yapılandırma

`%APPDATA%\BackupHarbor\config.yaml` konumunda bir YAML dosyası (`--config` veya
`BACKUPHARBOR_CONFIG` ile değiştirilebilir). Dosyadaki göreli yollar, dosyanın
bulunduğu dizine göre çözülür. Bilinmeyen anahtarlar reddedilir. En küçük örnek:

```yaml
version: 1
targets:
  - name: belgeler
    repository: 'D:\Yedekler\restic'
    password: {source: credential}      # credential | env | file
    rules:
      - {name: muhasebe, path: '**/muhasebe.sqlite', type: sqlite}
```

Tam başvuru: [docs/configuration.md](docs/configuration.md) (İngilizce).

## Komut satırı başvurusu

```
bharbor init [--force]
bharbor validate [--target AD]...
bharbor run [--target AD]...
bharbor report [--last | --list | --run KİMLİK] [--target AD] [--format json|html|txt] [--output DOSYA] [--force]
bharbor schedule install|status|remove [--frequency daily|weekly] [--time SS:DD] [--day monday..sunday]
bharbor secret set|delete|status AD [--stdin]
genel: --config DOSYA  --json  --no-color  --lang en|tr  --verbose  --quiet  --version  --help
```

| Çıkış kodu | Anlamı |
|---|---|
| 0 | Tüm hedefler başarılı. |
| 1 | Doğrulama çalıştı ve en az bir hedef başarısız oldu ya da yalnızca kısmen kanıtlandı. |
| 2 | Yapılandırma, bağımlılık veya çalıştırma hatası **ya da bir hedef test edilemedi**. |
| 3 | İptal edildi veya zaman aşımına uğradı. |

Sonuçlar karışık olduğunda öncelik: 3, sonra 1, sonra 2, sonra 0. İptal edilen bir
çalıştırma asla bozuk yedek olarak raporlanmaz.

## Raporlar

Her çalıştırma, `%LOCALAPPDATA%\BackupHarbor\reports` altında değiştirilemez bir JSON
kaydı olarak saklanır (şema `backupharbor.run/1`, bkz. [docs/report-schema.md](docs/report-schema.md)).
HTML (tek dosya, kendi içinde eksiksiz, yazdırılabilir) ve TXT istenildiğinde dışa
aktarılır veya her çalıştırmadan sonra otomatik kaydedilir. Var olan dosyaların üzerine
sessizce yazılmaz. Her iki arayüz de aynı geçmişi okur.

## Güvenlik ve gizlilik

- Yedek depo yalnızca okunur. restic depo kilitleri varsayılan olarak kapalıdır; depoya hiçbir şey yazılmaz.
- Geri yüklemeler BackupHarbor'ın oluşturduğu çalışma alanına gider; temizlik için sahiplik işareti gerekir, sembolik bağları ve junction'ları asla izlemez, sahiplik kanıtlanamıyorsa çalışmayı reddeder.
- Parolalar komut satırında, günlüklerde, raporlarda veya hata mesajlarında görünmez; bilinen sırlar ve kimlik bilgisi desenleri maskelenir.
- Alt süreçler kabuk kullanılmadan, ayrı argümanlarla başlatılır; iptal veya zaman aşımında süreç ağacıyla birlikte sonlandırılır.
- Telemetri yok, kendi başına ağ isteği yok.

Ayrıntılar ve tehdit modeli: [docs/security.md](docs/security.md) ve [SECURITY.md](SECURITY.md).

## Testler

```powershell
go test ./...
```

restic mevcutsa (`BH_TEST_RESTIC` veya PATH'te `restic`) gerçek bir restic deposuna karşı
testler de çalışır; restic yoksa bu testler başarılı değil, atlanmış olarak raporlanır.
`BH_TEST_SCHEDULER=1` gerçek Görev Zamanlayıcı'yı da dener. Tarayıcıyla sürülen bir arayüz
duman testi `tools/uidev` altındadır (bkz. [CONTRIBUTING.md](CONTRIBUTING.md)).

## Bilinen sınırlamalar

- Yalnızca Windows 10/11 x64. Desteklenen tek yedekleme arka ucu restic'tir.
- Zamanlanmış görev yalnızca kullanıcı oturum açıkken çalışır (etkileşimli belirteç); çünkü Windows Kimlik Bilgisi Yöneticisi erişimi kullanıcı profili gerektirir.
- Uzak depolar, kimlik bilgilerini (örneğin `AWS_*`) BackupHarbor'ı çalıştıran kullanıcının ortamında, zamanlanmış görev dahil, bulundurmalıdır.
- restic'in `--password-command` özelliği bilerek desteklenmez.
- Bir Windows snapshot'ı geri yüklendiğinde özgün yolun tamamı çalışma alanının altında, sürücü harfi bir klasör olarak başlayacak biçimde oluşur; bu yüzden içerik kuralları `**/` ile başlar.
- restic geri yüklemede üst klasörler için öznitelik hataları (zaman damgası, ACL) bildirebilir; bunlar ayrı sayılır ve raporlanır, içeriği sağlam bir geri yüklemeyi başarısız yapmaz.
- Açılan veya ayrıştırılan bir dosya, onu kullanan uygulamanın çalışacağını kanıtlamaz; PDF denetimleri yalnızca yapıseldir.
- İkili dosyalar kod imzalı değildir; Windows SmartScreen ilk açılışta uyarabilir.
- `docs/screenshots` içindeki görüntüler gerçek arayüzden ve gerçek arka uçtan, başsız bir tarayıcı üzerinden alınmıştır (bkz. oradaki README); yerel pencerenin fotoğrafı değildir.

## Ekran görüntüleri

Bkz. [docs/screenshots](docs/screenshots/README.md).

## Lisans

[MIT](LICENSE)
