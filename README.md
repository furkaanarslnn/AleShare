<p align="center"><img src="logo.svg" width="110" alt="AleShare logosu"></p>

<h1 align="center">AleShare</h1>

<p align="center">
  Telefonunuzun mobil internetini bilgisayarınızla, tabletinizle ve diğer telefonlarla<br>
  proxy ayarı gerektirmeden ve şifreli olarak paylaşın.
</p>

<p align="center">
  <a href="https://github.com/furkaanarslnn/AleShare/releases/latest"><img alt="Son sürüm" src="https://img.shields.io/github/v/release/furkaanarslnn/AleShare?label=s%C3%BCr%C3%BCm&color=0B6B80"></a>
  <img alt="Android 8.0 ve üstü" src="https://img.shields.io/badge/Android-8.0%2B-0B6B80">
  <img alt="Windows 10 ve 11" src="https://img.shields.io/badge/Windows-10%20%2F%2011-0B6B80">
</p>

<p align="center">
  <a href="https://github.com/furkaanarslnn/AleShare/releases/latest/download/AleShare.apk"><img alt="Android için indir" src="https://img.shields.io/badge/Android-APK%20indir-0B6B80?style=for-the-badge"></a>
  <a href="https://github.com/furkaanarslnn/AleShare/releases/latest/download/AleShare.exe"><img alt="Windows için indir" src="https://img.shields.io/badge/Windows-EXE%20indir-0B6B80?style=for-the-badge"></a>
</p>

AleShare, Android telefonunuzu kablosuz bir internet paylaşım noktasına dönüştürür. Telefon kendi Wi-Fi ağını açar; bilgisayar, tablet ya da başka bir telefon bu ağa katılır ve interneti telefonun mobil verisinden alır. Cihazlar arasındaki bağlantı WireGuard ile, adres sorguları (DNS) HTTPS ile şifrelenir. Bağlanan cihazda proxy ayarı yapmak gerekmez; bütün uygulamalar olağan biçimde çalışır.

Yapımcı: **furkanyela**

> [!NOTE]
> Hazır uygulamalar [Sürümler sayfasındadır](https://github.com/furkaanarslnn/AleShare/releases/latest). Bu sayfadaki dosyalar kaynak koddur; uygulamayı kullanmak için kaynak kodu indirmeniz gerekmez.

## İçindekiler

- [Özellikler](#özellikler)
- [Gereksinimler](#gereksinimler)
- [Kurulum](#kurulum)
- [Kullanım](#kullanım)
- [Güncellemeler](#güncellemeler)
- [Önemli notlar](#önemli-notlar)
- [Sorun giderme](#sorun-giderme)
- [Kaldırma](#kaldırma)
- [Windows ve Android uyarıları](#windows-ve-android-uyarıları)
- [Gizlilik](#gizlilik)
- [Nasıl çalışır](#nasıl-çalışır)
- [Kaynaktan derleme](#kaynaktan-derleme)
- [Yapay zekâ kullanımı](#yapay-zekâ-kullanımı)
- [Lisans](#lisans)
- [Sorumluluk reddi](#sorumluluk-reddi)

## Özellikler

**Paylaşım**

- Tek dokunuşla paylaşım: telefon kendi Wi-Fi ağını açar, ağ adı ve parolası siz değiştirmedikçe aynı kalır.
- Bağlanan cihazda proxy ayarı yoktur; tarayıcı, oyun, görüntülü görüşme ve diğer bütün uygulamalar doğrudan çalışır.
- Cihazlar arasındaki bağlantı WireGuard ile, DNS sorguları HTTPS ile şifrelenir.
- 2,4 GHz ve 5 GHz bant seçimi.
- Ana ekran aracı (widget) ve bildirim panelindeki hızlı ayar düğmesiyle uygulamayı açmadan başlatma ve durdurma.

**Cihazlar**

- Windows bilgisayar, Android telefon ve tabletler QR kodla eklenir; her cihazın kendi anahtarı vardır ve tek dokunuşla kaldırılabilir.
- iPhone, Mac ve Linux desteği geliştirilme aşamasındadır.
- Cihaz başına internet duraklatma, günlük veri sınırı, hız sınırı ve internetin kapalı olacağı saatler.
- Süreli ya da veri sınırlı misafir erişimi.

**Veri takibi**

- Bugün paylaşılan veri, cihaz başına kullanım ve anlık indirme/yükleme hızı.
- Günlük veri sınırı: sınıra yaklaşınca bildirim gelir, dolunca paylaşım durur.
- Aylık paket takibi: kalan veri, kalan gün ve son 30 günün grafiği.
- Hız testi: mobil internetin indirme, yükleme hızı ve gecikmesi; bilgisayardan başlatıldığında aradaki Wi-Fi bağlantısı da ölçülür.

**Koruma ve tasarruf**

- Reklam ve izleyici engelleme: bağlı bütün cihazlar için geçerlidir.
- Veri tasarrufu: paylaşım sırasında işletim sistemi ve oyun güncellemeleri ile mağaza indirmeleri bekletilir.
- Akıllı öncelik: görüşme ya da oyun sırasında büyük indirmeler yavaşlatılır.
- AleDPI modu: güvenli bağlantıların ilk paketini parçalara bölerek gönderir; bazı ağlarda açılmayan sitelere bağlanmaya yardımcı olabilir. Kapalı gelir.
- Pil ve ısı koruması: telefon ısındığında paylaşım yavaşlar, pil azaldığında uyarı verilir ve paylaşım durdurulur.

**Windows programı**

- Kurulum sihirbazı: WireGuard'ı kurar, telefonun ağını bulur ve QR kodla eşleşir.
- Otomatik bağlanma: telefonda paylaşım başladığında bilgisayar kendiliğinden bağlanır, bittiğinde önceki ağına döner.
- Durum penceresi ve saatin yanındaki simge: telefonun şarjı, sinyali, anlık hız, bugünkü veri; telefonun ayarları bilgisayardan değiştirilebilir.

**Güncelleme**

- Yeni sürüm yayımlandığında uygulama ve Windows programı haber verir; indirme siz isteyince başlar.
- Bir cihazı güncellemeniz yeterlidir: diğer cihazlar yeni sürümü paylaşan telefon üzerinden, mobil veri harcamadan alır.

## Gereksinimler

| Cihaz | Gereksinim |
| --- | --- |
| Paylaşan telefon | Android 8.0 ya da üstü, 64 bit (arm64) işlemci, mobil veri |
| Android telefon ya da tablet (bağlanan) | Android 8.0 ya da üstü, 64 bit (arm64) işlemci |
| Windows bilgisayar | Windows 10 ya da 11 (64 bit), Wi-Fi |
| iPhone, Mac, Linux | Geliştirilme aşamasında |

Windows programı, tünel için resmî [WireGuard for Windows](https://www.wireguard.com/install/) paketini kullanır; yoksa kurulum sırasında kendisi indirip kurar.

## Kurulum

Dosyalar [Sürümler sayfasındadır](https://github.com/furkaanarslnn/AleShare/releases/latest):

| Cihaz | Dosya |
| --- | --- |
| Android telefon ya da tablet | `AleShare.apk` |
| Windows 10 / 11 bilgisayar | `AleShare.exe` |

### Telefon (interneti paylaşan)

1. APK dosyasını telefona indirin ve açın. Android ilk seferde "bilinmeyen uygulamaları yükleme" izni ister; dosyayı açtığınız uygulamaya (Dosyalarım, Chrome vb.) bu izni verin.
2. AleShare'i açın ve **Paylaşımı başlat**'a dokunun. İstenen izinleri (bildirim, yakındaki cihazlar) verin.
3. Üstte pil uyarısı çıkarsa **Düzelt**'e dokunun; böylece ekran kapalıyken de paylaşım sürer.
4. **Wi-Fi ağı** kartında ağ adı (`DIRECT-…`) ve parola görünür.

Bundan sonra her kullanımda uygulamayı açıp **Paylaşımı başlat**'a, bitince **Durdur**'a dokunmanız yeterlidir.

### Windows bilgisayar

1. Telefonda paylaşımı başlatın.
2. Bilgisayar internete bağlıyken (ilk kurulumda WireGuard indirilir) `AleShare.exe` dosyasına çift tıklayın. Açılan pencerede **Kuruluma başla**'ya tıklayın ve Windows'un onay penceresini onaylayın.
3. Pencere dört adımı sırayla yürütür:
   - **WireGuard programı:** yoksa resmî paket indirilip kurulur.
   - **Telefonun Wi-Fi ağı:** `DIRECT-…` ağı bulunur ve bilgisayar ona bağlanır. Windows ağı tanımıyorsa parola bir kez sorulur; telefonda **Wi-Fi ağı** kartındaki parolayı yazın.
   - **QR kodla eşleşme:** pencerede bir QR kod çıkar. Telefonda **Cihazlar → Ekle → Bilgisayar**'a dokunun ve kodu okutun.
   - **Kurulumun tamamlanması.**
4. AleShare artık Başlat menüsündedir. Telefonda paylaşım başladığında bilgisayar kendiliğinden bağlanır; paylaşım bittiğinde önceki Wi-Fi ağına döner.

### Tablet ya da ikinci Android telefon

1. Aynı APK'yı o cihaza da kurun.
2. Paylaşan telefonda **Cihazlar → Ekle → Telefon veya tablet**'e dokunun ve cihaza bir ad verin.
3. Diğer cihazda uygulamayı açın, **Bağlan → QR kodu tara** ile telefondaki kodu okutun. Tarama yapılamıyorsa paylaşan telefonda **Bağlantıyı kopyala**, diğer cihazda **Bağlantıyı yapıştır** kullanılabilir.
4. **Kaydet ve bağlan**'a dokunun; Android'in VPN iznini ve ağa katılma iznini onaylayın.

Cihaz bundan sonra telefonun ağı yakındayken kendiliğinden katılır, uzaklaşınca bağlantıyı bırakır.

### iPhone, Mac ve Linux

Bu cihazlar için destek geliştirilme aşamasındadır.

## Kullanım

Uygulamanın dört bölümü ekranın altındadır: **Paylaş**, **Veri**, **Ayarlar** ve **Bağlan**.

### Paylaş

- **Paylaşımı başlat / Durdur:** paylaşımı açar ve kapatır. Açıkken bağlı cihaz sayısı ile anlık indirme ve yükleme hızı görünür.
- **Wi-Fi ağı kartı:** ağ adı, parola, kullanılan bant ve kanal. **QR ile bağlan** ağın QR kodunu gösterir, **Değiştir** ağ adını ve parolasını değiştirir.
- **Cihazlar:** eklenmiş cihazların listesi; her cihazın bugünkü kullanımı adının altında yazar.
- **Ekle:** **Bilgisayar** (Windows programının gösterdiği kodu okutur), **Telefon veya tablet** (diğer cihaz için QR kod üretir), **Misafir** (süreli ya da veri sınırlı erişim).

Bir cihaza dokunduğunuzda o cihaza özel ayarlar açılır:

- **İnterneti duraklat:** cihazı internetten hemen keser; cihaz bağlı kalır, **Devam ettir** ile anında döner.
- **Günlük sınır:** dolduğunda cihaz gece yarısına kadar durur.
- **Hız sınırı:** 1, 2, 5, 10 ya da 20 Mbit/s.
- **İnternetin kapalı olduğu saatler:** cihaz her gün seçtiğiniz saatler arasında internete giremez.
- **Kaldır:** cihazın anahtarını iptal eder; cihaz yeniden eklenmeden bağlanamaz.

**Misafir erişimi:** bir ad, süre (1 saat, 3 saat, 1 gün, süresiz) ve veri (500 MB, 1 GB, 2 GB, sınırsız) seçilir. Misafirin cihazında resmî WireGuard uygulaması bulunmalıdır; önce Wi-Fi kodu, sonra WireGuard kodu okutulur. Süre ya da veri dolduğunda misafir kendiliğinden silinir.

### Veri

- **Bugün paylaşılan veri:** o gün paylaşımdan geçen mobil veri; gece yarısı sıfırlanır. **Sınır koy** ile günlük sınır (1–50 GB) belirlenir: %90'ına gelindiğinde bildirim gelir, dolduğunda paylaşım durur.
- **Aylık paket:** paketinizin büyüklüğünü ve yenilendiği günü girdiğinizde bu dönemki kullanım, kalan veri, kalan gün ve son 30 günün grafiği gösterilir. İsterseniz telefonun kendi mobil veri kullanımı da hesaba katılır; bunun için Android'in "kullanım erişimi" izni gerekir.
- **Hız testi:** mobil internetin indirme, yükleme hızını ve gecikmesini ölçer (yaklaşık 20 saniye, en fazla 55 MB kadar veri).
- **Bu oturum:** paylaşım açıldığından beri geçen veri ve DNS sorgularının şifreli gidip gitmediği.

### Ayarlar

| Grup | Ayar | Ne yapar |
| --- | --- | --- |
| Koruma ve tasarruf | Reklam ve izleyici engelleme | Bağlı bütün cihazlarda reklam ve izleyici adreslerini engeller. Açık gelir. |
| | Veri tasarrufu | Paylaşım sırasında sistem ve oyun güncellemelerini bekletir. Kapalı gelir. |
| | Akıllı öncelik | Görüşme ya da oyun sırasında büyük indirmeleri yavaşlatır. Açık gelir. |
| | AleDPI modu | Güvenli bağlantıların ilk paketini parçalara bölerek gönderir. Kapalı gelir. |
| Telefon | Pil ve ısı koruması | Telefon ısındığında paylaşımı yavaşlatır; pil %15'e indiğinde uyarır, %5'e indiğinde paylaşımı durdurur. |
| | Kimse bağlı değilken kapat | Seçilen süre boyunca hiçbir cihaz bağlanmazsa paylaşımı kapatır. |
| Wi-Fi ağı | Wi-Fi ağını uygulama açsın | Kapatıldığında telefonun kendi hotspot'u ya da başka bir ağ kullanılabilir. |
| | Bant | 2,4 GHz her cihazın gördüğü banttır; 5 GHz daha hızlıdır. |
| Kısayol | Paylaşım düğmesi | Bildirim paneline hızlı ayar düğmesi ekler. |
| Güncelleme | Yeni sürümleri denetle | Yeni sürüm yayımlandığında haber verir. **Şimdi denetle** elle baktırır. |
| Gelişmiş | Bağlantı adresi | QR kodlara yazılan adres; çoğu kullanıcının değiştirmesi gerekmez. |
| Hakkında | Açık kaynak lisansları | Uygulamada kullanılan açık kaynak bileşenlerin lisans metinleri. |

Ana ekran aracı için ana ekranda boş bir yere uzun basıp **Widget'lar → AleShare** yolunu izleyin. Araca dokunmak paylaşımı uygulamayı açmadan başlatır ve durdurur.

### Bağlan

Bu bölüm, cihaz internetini başka bir telefondan alacaksa kullanılır (bkz. [Tablet ya da ikinci Android telefon](#tablet-ya-da-ikinci-android-telefon)).

- Bağlantının durumu, anlık hız ve bu oturumda indirilen ve yüklenen veri görünür.
- Paylaşan telefonda daha yeni bir sürüm varsa yeni sürümün hazır olduğunu bildiren bir kart çıkar; **İndir ve kur** dosyayı telefonun ağından, mobil veri harcamadan alır.
- Bağlıyken paylaşıma özgü ayarlar kilitlenir; **Durdur**'a dokunduğunuzda açılır.
- Bağlantının hiç kopmamasını isterseniz Android'in VPN ayarlarında AleShare için **Her zaman açık VPN**'i açabilirsiniz. **VPN olmadan bağlantıları engelle** seçeneğini kapalı bırakın; aksi hâlde paylaşan telefon yakında değilken cihazın interneti kesilir.

### Windows programı

Pencere Başlat menüsünden ya da `AleShare.exe` dosyasına çift tıklanarak açılır.

- **Durum:** bilgisayarın telefona bağlı olup olmadığı. Bilgisayar telefonun ağında değilse **Telefonun ağına bağlan** düğmesi çıkar.
- **Telefon kartı:** telefonun şarjı, şebekesi ve sinyali, anlık hız, bugünkü veri ve sınırı, aylık paket.
- **Cihazlar:** bağlı cihazları duraklatma ve devam ettirme.
- **Hız testi:** önce bilgisayarla telefon arasındaki Wi-Fi, ardından telefonun mobil interneti ölçülür.
- **Telefon ayarları:** reklam engelleme, veri tasarrufu, akıllı öncelik, AleDPI modu, pil ve ısı koruması ile günlük veri sınırı bilgisayardan değiştirilebilir.
- **Otomatik bağlan:** açıkken bilgisayar telefonun ağına kendiliğinden geçer; kapalıyken ağa siz bağlanırsınız, internet yine kendiliğinden gelir.
- **Gelişmiş:** **Yeniden eşleştir**, **Kaldır** ve **Son kayıtlar**.

**Saatin yanındaki simge** oturum açıldığında kendiliğinden başlar: renkliyken telefona bağlı, griyken bağlı değildir. Üzerine gelindiğinde hız ve telefonun pil durumu görünür; tıklandığında pencere açılır. Bağlantı koptuğunda ya da geri geldiğinde, telefonun pili azaldığında, telefon ısındığında ve veri sınırına yaklaşıldığında Windows bildirimi gösterilir.

Telefonun ağı Windows'ta **tarifeli bağlantı** olarak işaretlenir; böylece Windows Update, OneDrive ve Mağaza büyük indirmelerini bu ağda kendiliğinden erteler. Bu ayarı Windows'un Wi-Fi ayarlarından kapatabilirsiniz.

### Komut satırı

Pencerenin yaptıkları komut isteminden de yapılabilir:

| Komut | İşlev |
| --- | --- |
| `AleShare.exe status` | Durumu gösterir |
| `AleShare.exe check` | Daha yeni bir sürüm olup olmadığına bakar |
| `AleShare.exe autojoin on` / `off` | Otomatik bağlanmayı açar ya da kapatır |
| `AleShare.exe upgrade` | Bu dosyadaki sürümü mevcut kurulumun üzerine kurar; eşleşme korunur |
| `AleShare.exe uninstall` | Programı kaldırır |
| `AleShare.exe licenses` | Açık kaynak bileşenlerin lisans metinlerini yazdırır |

### Ağ adını ve parolasını değiştirme

Paylaşım açıkken **Wi-Fi ağı** kartında **Değiştir**'e dokunun.

- **Ad:** Android, bu tür ağların adının `DIRECT-` ile başlamasını şart koşar; devamını siz belirlersiniz (en çok 25 karakter; harf, rakam, `-` ve `_`).
- **Parola:** 8–63 karakter. **Rastgele parola** güçlü bir parola üretir.

Telefon yeni adı ve parolayı önce o an bağlı cihazlara bildirir; bu cihazlar yeni ağa kendiliğinden geçer. O sırada kapalı ya da uzakta olan cihazlar bir kez yeniden katılır: bilgisayarda Windows'tan yeni ağa bağlanmak, tablette **Bağlan → Wi-Fi kodunu tara** ile telefondaki kodu okutmak yeterlidir. Eşleşmeler korunur.

## Güncellemeler

- Uygulama ve Windows programı günde bir iki kez Sürümler sayfasına bakar. Yeni sürüm yayımlandığında bildirim gelir; telefonda **İndir**, ardından **Kur**, bilgisayarda **Güncelle** demeniz yeterlidir. İndirme siz istemeden başlamaz.
- Bir cihazı güncellemeniz yeterlidir. Paylaşan telefon güncellendiğinde bilgisayar yeni programı telefondan alıp kendiliğinden kurar; tabletlerde **Bağlan** bölümünde kurulum kartı çıkar. Bilgisayarı ya da bir tableti güncellediğinizde de yeni sürüm telefona ve oradan diğer cihazlara ulaşır. Bu aktarımlar telefonun kendi ağında yapılır, mobil veri harcamaz.
- Android her kurulumda onay ister; telefon ve tabletler sormadan güncellenmez.
- Güncellemeler yalnızca bu projenin imzasını taşıyorsa kurulur: Android APK'nın imzasını, Windows programı kendi sürüm imzasını denetler.
- Elle güncellemek için yeni dosyayı eskisinin üzerine kurun. Uygulamayı kaldırıp yeniden kurmayın; aksi hâlde eşleşmeler, ağ adı ve parola silinir.
- Denetimi telefonda **Ayarlar → Güncelleme** bölümünden kapatabilirsiniz.

## Önemli notlar

> [!IMPORTANT]
> Paylaşım telefonun kendi Wi-Fi ağıyla yapılır; bu nedenle telefonda Wi-Fi açık olmalıdır (bir ağa bağlı olması gerekmez). Wi-Fi kapatıldığında ya da uçak modu açıldığında paylaşım kendiliğinden durur.

- **Pil kısıtlaması:** bazı telefonlar ekran kapalıyken arka plandaki uygulamaları durdurur. Uygulamanın üstünde çıkan pil uyarısındaki **Düzelt** düğmesi bu kısıtlamayı kaldırır.
- **Samsung Otomatik Engelleyici:** açıkken mağaza dışından uygulama kurulamaz. Kurulum için **Ayarlar → Güvenlik ve gizlilik → Otomatik Engelleyici** bölümünden kapatıp kurulumdan sonra yeniden açabilirsiniz.
- **5 GHz:** daha hızlıdır, ancak bazı eski Wi-Fi kartları bu ağı göremez. Bilgisayarınız ağı göremiyorsa **Ayarlar → Wi-Fi ağı → Bant** bölümünden 2,4 GHz'e geçin.
- **Aynı anda tek görev:** paylaşım açıkken **Bağlan** bölümü, bağlıyken paylaşım ayarları kilitlidir; bir telefon aynı anda hem paylaşıp hem başka bir telefona bağlanamaz.
- **Sayaç farkı:** uygulamanın gösterdiği veri miktarı, hattınızın faturasında görünen değerden biraz farklı olabilir (paket başlıkları ve telefonun kendi kullanımı nedeniyle).
- **Telefonu yeniden başlatma:** telefon yeniden başlatıldığında paylaşımı elle başlatmanız gerekir.

## Sorun giderme

**Bilgisayar telefonun ağını görmüyor.** Windows, başka bir Wi-Fi ağına bağlıyken yeni ağları geç tarar. Wi-Fi'yi bir kez kapatıp açın ya da pencerede **Otomatik bağlan**'ı açın. Bilgisayarda "Mobil etkin nokta" açıksa kapatın. Ağ yine görünmüyorsa telefonda 2,4 GHz bandını deneyin.

**Bağlı görünüyor ama internet yok.** Telefonda uygulamanın **Paylaşım açık** dediğini ve mobil verinin çalıştığını denetleyin. Bilgisayarda Windows'un proxy ayarı açık kalmışsa kapatın (**Ayarlar → Ağ ve İnternet → Proxy**).

**Telefondaki uygulama kapandı.** Bilgisayar bunu bir dakika içinde anlar, tüneli kapatır ve önceki ağına döner; tablet "Telefon yanıt vermiyor" der. Paylaşım yeniden başladığında ikisi de kendiliğinden geri döner; bu bir iki dakika sürebilir.

**Tablet bağlanmak için yeniden onay istiyor.** Paylaşan telefon paylaşımı durdurup yeniden başlattığında Android, ağa katılmak için yeniden onay isteyebilir. Çıkan **Bağlan** penceresine dokunmanız yeterlidir.

**Bazı siteler açılmıyor.** Paylaşan telefonda **Ayarlar → Koruma ve tasarruf → AleDPI modu**nu açıp yeniden deneyin. Her ağda sonuç vermeyebilir; gerek kalmadığında kapatabilirsiniz. Bir site ya da uygulama reklam engelleme açıkken bozuluyorsa **Reklam ve izleyici engelleme**'yi kapatıp deneyin.

**DNS sorguları şifresiz gidiyor.** **Veri → Bu oturum** kartında "DNS sorguları şifreli" yazmalıdır. "N sorgu şifresiz gitti" görürseniz o sırada şifreli DNS sunucularına ulaşılamamış demektir; bağlantı düzeldiğinde sorgular yeniden şifreli gider.

**Kurulum penceresi açılmıyor.** Pencere, Windows'un Edge WebView2 bileşeniyle çizilir (Windows 11'de her zaman vardır). Bulunmadığında kurulum komut penceresinde yürür.

### Hata bildirimi

Çözemediğiniz bir sorunla karşılaşırsanız [Issues](https://github.com/furkaanarslnn/AleShare/issues) sekmesinden bildirebilirsiniz. Bildirime şunları eklemeniz çözümü hızlandırır:

- Uygulamanın en altında yazan sürüm numarası.
- Telefonun ve bilgisayarın işletim sistemi sürümü.
- Sorunun hangi adımda ortaya çıktığı ve ekranda ne yazdığı.
- Bilgisayar tarafındaki sorunlarda pencerenin **Gelişmiş → Son kayıtlar** bölümündeki satırlar.

Lütfen bildirimlere Wi-Fi parolanızı ya da QR kodlarınızın görüntüsünü eklemeyin.

## Kaldırma

- **Windows:** AleShare penceresinde **Gelişmiş → Kaldır**'a tıklayın ya da `AleShare.exe uninstall` komutunu çalıştırın. Program, tünel, hizmet, kısayol ve eşleşme silinir. WireGuard programı bilgisayarda kalır; isterseniz Windows'un **Uygulamalar** bölümünden ayrıca kaldırabilirsiniz.
- **Android:** uygulamayı diğer uygulamalar gibi kaldırın. Eşleşmeler, ağ adı ve parola uygulamayla birlikte silinir.
- Bir cihazı kaldırdıktan sonra paylaşan telefonda **Cihazlar** listesinden de silmeniz önerilir.

## Windows ve Android uyarıları

`AleShare.exe` bir kod imzalama sertifikasıyla imzalanmamıştır. Bu nedenle Windows SmartScreen ilk açılışta "bilinmeyen yayıncı" uyarısı gösterebilir; **Ek bilgi → Yine de çalıştır** ile devam edebilirsiniz. Android de mağaza dışından kurulan uygulamalarda benzer bir uyarı gösterebilir.

Dosyaları yalnızca bu deponun [Sürümler sayfasından](https://github.com/furkaanarslnn/AleShare/releases) indirin.

Bazı antivirüs programları, yeni ve az indirilen programları yalnızca bu nedenle şüpheli olarak işaretleyebilir. Kuşku duyduğunuzda kaynak kodu inceleyebilir ve programı kendiniz derleyebilirsiniz.

## Gizlilik

- Uygulamada hesap, reklam ya da analiz yoktur; projeye ait bir sunucu bulunmaz.
- Hangi cihazın hangi adrese gittiği kaydedilmez; yalnızca veri miktarını gösteren sayaçlar tutulur.
- Her cihazın kendi anahtarı vardır ve paylaşan telefondan kaldırıldığında anında geçersiz olur.
- Sürüm denetimi GitHub'a yapılan küçük bir istektir; isterseniz kapatabilirsiniz.
- Şifreleme, cihazlarınız ile paylaşan telefon arasındaki bağlantıyı kapsar. Telefondan internete çıkan trafik, mobil hattınız üzerinden her zamanki gibi gider.

## Nasıl çalışır

- **Ağ:** paylaşan telefon, Android'in Wi-Fi Direct özelliğiyle kendi kablosuz ağını açar. Bağlanacak cihazlar bu ağa olağan bir Wi-Fi ağı gibi katılır.
- **Tünel:** telefonda kullanıcı alanında çalışan bir WireGuard sunucusu ([wireguard-go](https://git.zx2c4.com/wireguard-go/about/)) bulunur. Her cihaz kendi anahtarıyla bu sunucuya bağlanır; cihazın bütün trafiği tünelden geçer.
- **Ağ geçidi:** tünelden gelen paketler, [gVisor](https://gvisor.dev/)'ın kullanıcı alanı ağ yığınında bağlantılara dönüştürülür ve telefonun mobil verisi üzerinden internete iletilir. Telefonda root yetkisi gerekmez.
- **DNS:** adres sorgularını telefon yanıtlar ve HTTPS üzerinden şifreli olarak çözer. Reklam ve izleyici engelleme bu aşamada uygulanır.
- **Eşleşme:** QR kod, cihazın tünele katılması için gereken bilgileri taşır. Her cihaza ayrı bir anahtar verilir; cihaz listeden kaldırıldığında anahtarı geçersiz olur.
- **Windows:** program, resmî WireGuard for Windows hizmetini kullanır. Küçük bir izleme hizmeti, bilgisayar telefonun ağına girdiğinde tüneli açar, ayrıldığında kapatır.
- **Android (bağlanan cihaz):** uygulama, Android'in VPN arayüzü üzerinden aynı WireGuard tünelini kurar.
- **Güncelleme:** her sürüm GitHub'da yayımlanır. Windows programı Ed25519 sürüm imzasını, Android ise APK'nın imzasını denetler; imzası tutmayan dosya kurulmaz.

## Kaynaktan derleme

Depo yalnızca kaynak kodu içerir:

| Klasör | İçerik |
| --- | --- |
| `android/` | Android uygulaması (Kotlin, Jetpack Compose) |
| `core/` | Go çekirdeği: WireGuard sunucusu, ağ geçidi, eşleşme ve güncelleme dağıtımı; `gomobile` ile Android uygulamasına bağlanır |
| `core/cmd/hotspot-pc/` | Windows programı (`AleShare.exe`) |
| `core/cmd/notices/` | Programlara giren açık kaynak bileşenlerin lisans metinlerini toplayan araç |
| `build-android.ps1` | Derleme betiği |

Gerekenler: Windows, PowerShell, [Go](https://go.dev/), JDK 17 ve NDK'sı kurulu Android SDK. Derlemek için depo klasöründe şu komutu çalıştırın:

```powershell
.\build-android.ps1
```

Bu komut hata ayıklama (debug) APK'sını üretir. `-Release` seçeneği yayın imza anahtarlarını ister. Bu anahtarlar depoda yoktur ve hiçbir zaman olmayacaktır; bu nedenle başka bir yerde derlenen APK, yayımlanmış uygulamanın üzerine kurulamaz.

## Yapay zekâ kullanımı

AleShare'in arayüzü oluşturulurken yapay zekâ araçlarından yararlanılmıştır.

## Lisans

Telif hakkı © 2026 furkanyela. Tüm hakları saklıdır.

Sürümler sayfasındaki uygulamaları indirip kendi cihazlarınızda kullanmanız serbesttir. Kaynak kod, incelenebilmesi amacıyla yayımlanmıştır; yazılı izin olmadan kopyalanamaz, değiştirilemez, dağıtılamaz ve başka bir üründe kullanılamaz. Ayrıntılar [LICENSE](LICENSE) dosyasındadır.

Uygulamalarda kullanılan açık kaynak bileşenler kendi lisanslarına tabidir. Lisans metinleri uygulamaların içindedir (telefonda **Ayarlar → Hakkında → Açık kaynak lisansları**, bilgisayarda pencerenin altındaki **Açık kaynak lisansları**) ve depoda da bulunur: [Android uygulaması](android/app/src/main/assets/licenses.txt), [Windows programı](core/cmd/hotspot-pc/ui/licenses.txt).

WireGuard, Jason A. Donenfeld'in tescilli markasıdır. AleShare, WireGuard projesiyle bağlantılı değildir.

## Sorumluluk reddi

> [!IMPORTANT]
> AleShare kişisel bir projedir ve "olduğu gibi", hiçbir garanti verilmeksizin sunulur. Yazılımın kullanımından doğabilecek veri kaybı, ek ücret ya da başka zararlardan yapımcı sorumlu tutulamaz. Yazılımı, mobil hattınızın kullanım koşullarına ve bulunduğunuz yerde yürürlükte olan kurallara uygun biçimde kullanmak kullanıcının sorumluluğundadır.
