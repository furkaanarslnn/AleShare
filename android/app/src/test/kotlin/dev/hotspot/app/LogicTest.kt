package dev.hotspot.app

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** The parts of the app that are plain logic, tested on the computer without a phone. */
class LogicTest {
  private val saved = WifiCredentials("DIRECT-HS-Q4TZ", "abcdEFGH2345jkmn")
  private val next = WifiCredentials("DIRECT-HS-Ev", "yeni-parola-123")

  @Test
  fun usageCountsGrowthAndSurvivesARestartOfTheCounters() {
    assertEquals(500, counterDelta(1500, 1000))
    assertEquals("sharing started again: all of it is new", 300L, counterDelta(300, 1000))
    assertEquals(0, counterDelta(1000, 1000))
  }

  @Test
  fun dailyLimitWarnsOnceThenStopsOnce() {
    val gb = 1024L * 1024 * 1024
    assertEquals(LimitAction.None, limitAction(5 * gb, 0, warned = false, stopped = false))
    assertEquals(LimitAction.None, limitAction(gb / 2, gb, warned = false, stopped = false))
    assertEquals(LimitAction.Warn, limitAction(gb * 9 / 10, gb, warned = false, stopped = false))
    assertEquals(LimitAction.None, limitAction(gb * 95 / 100, gb, warned = true, stopped = false))
    assertEquals(LimitAction.Stop, limitAction(gb, gb, warned = true, stopped = false))
    assertEquals("started again after the stop", LimitAction.None, limitAction(2 * gb, gb, warned = true, stopped = true))
  }

  @Test
  fun trafficLinesAndLimitLabels() {
    assertEquals(mapOf("Laptop" to 25000L, "Tab S9" to 7L), parseTraffic("Laptop\t25000\nTab S9\t7\n\nbroken"))
    assertEquals("5 GB", limitLabel(5120))
    assertEquals("500 MB", limitLabel(500))
  }

  @Test
  fun wifiCodeRoundTripsSpecialCharacters() {
    val tricky = WifiCredentials("DIRECT-HS-a;b", """p:a\s,s"w;rd""")
    val code = WifiNetwork.qrText(tricky)
    assertEquals("""WIFI:T:WPA;S:DIRECT-HS-a\;b;P:p\:a\\s\,s\"w\;rd;;""", code)
    assertEquals(tricky, WifiNetwork.parseQr(code))
  }

  @Test
  fun wifiCodeOfAnotherNetworkOrBadPasswordIsRefused() {
    assertNull("not a Wi-Fi Direct network", WifiNetwork.parseQr("WIFI:T:WPA;S:EvAgi;P:abcdefgh;;"))
    assertNull("password too short", WifiNetwork.parseQr("WIFI:T:WPA;S:DIRECT-HS-x;P:abc;;"))
    assertNull("no password", WifiNetwork.parseQr("WIFI:T:WPA;S:DIRECT-HS-x;;"))
    assertNull("not a Wi-Fi code", WifiNetwork.parseQr("hotspot://join?d=abc"))
    assertNotNull(WifiNetwork.parseQr("WIFI:S:DIRECT-HS-x;T:WPA;P:abcdefgh;H:false;;"))
  }

  @Test
  fun namesAndPasswordsFollowTheRules() {
    assertNull(WifiNetwork.nameProblem("Ev_Agi-2"))
    assertNotNull("empty", WifiNetwork.nameProblem(""))
    assertNotNull("Turkish letter", WifiNetwork.nameProblem("Çalışma"))
    assertNotNull("space", WifiNetwork.nameProblem("ev agi"))
    assertNotNull("too long", WifiNetwork.nameProblem("a".repeat(WifiNetwork.MAX_NAME + 1)))
    assertNull(WifiNetwork.nameProblem("a".repeat(WifiNetwork.MAX_NAME)))
    assertEquals(32, (WifiNetwork.PREFIX + "a".repeat(WifiNetwork.MAX_NAME)).length)
    // Android's "DIRECT-xy": the name's first two characters are the xy.
    assertEquals("DIRECT-", WifiNetwork.PREFIX)
    assertNull(WifiNetwork.nameProblem("Ev"))
    assertNull(WifiNetwork.nameProblem("Ofis"))
    assertNotNull("one character", WifiNetwork.nameProblem("E"))
    assertNotNull("a dash among the first two", WifiNetwork.nameProblem("E-v"))
    assertNotNull(WifiNetwork.nameProblem("_Ev"))
    assertTrue("a name from before 1.0.31 is still read", WifiNetwork.parseQr("WIFI:T:WPA;S:DIRECT-HS-K7PM;P:abcdefgh;;") != null)
    assertTrue(WifiNetwork.parseQr("WIFI:T:WPA;S:DIRECT-Ofis;P:abcdefgh;;") != null)

    assertNull(WifiNetwork.passwordProblem("12345678"))
    assertNotNull("short", WifiNetwork.passwordProblem("1234567"))
    assertNotNull("long", WifiNetwork.passwordProblem("a".repeat(64)))
    assertNotNull("Turkish letter", WifiNetwork.passwordProblem("şifre1234"))
    assertEquals(16, WifiNetwork.randomPassword().length)
    assertNull(WifiNetwork.passwordProblem(WifiNetwork.randomPassword()))
  }

  @Test
  fun searchWithoutAChangeKeepsTheSavedNetwork() {
    assertEquals(saved, afterSearch(saved, null, saved, nextFirst = false))
    assertEquals(saved, afterSearch(saved, null, null, nextFirst = false))
  }

  @Test
  fun searchAfterTheSwitchLooksForTheNewNetworkFirstThenBoth() {
    // Right after losing the phone: only the new network, however often the search ends.
    assertEquals(next, afterSearch(saved, next, next, nextFirst = true))
    assertEquals(next, afterSearch(saved, next, saved, nextFirst = true))
    // Later: the two names in turn, in case the phone went back to the old one.
    assertEquals(saved, afterSearch(saved, next, next, nextFirst = false))
    assertEquals(next, afterSearch(saved, next, saved, nextFirst = false))
  }

  @Test
  fun passwordOnlyChangeIsTwoDifferentNetworks() {
    val newPassword = WifiCredentials(saved.ssid, "baska-parola-9")
    assertEquals(saved, afterSearch(saved, newPassword, newPassword, nextFirst = false))
    assertEquals(newPassword, afterSearch(saved, newPassword, saved, nextFirst = false))
  }

  @Test
  fun bandAndChannelFromTheFrequency() {
    assertEquals("2,4 GHz · kanal 1", bandLabel(2412))
    assertEquals("2,4 GHz · kanal 6", bandLabel(2437))
    assertEquals("5 GHz · kanal 36", bandLabel(5180))
    assertEquals("5 GHz · kanal 149", bandLabel(5745))
    assertNull(bandLabel(0))
  }

  @Test
  fun aDeviceLimitStopsAtItsSizeOnly() {
    val gb = 1024L * 1024 * 1024
    assertFalse("no limit", overLimit(10 * gb, 0))
    assertFalse(overLimit(gb - 1, gb))
    assertTrue(overLimit(gb, gb))
  }

  @Test
  fun sharingComesBackAfterAnUpdateOnly() {
    assertTrue(sharingAfter(android.content.Intent.ACTION_MY_PACKAGE_REPLACED, wasSharing = true))
    assertFalse("it was off", sharingAfter(android.content.Intent.ACTION_MY_PACKAGE_REPLACED, wasSharing = false))
    assertFalse("a restart of the phone", sharingAfter(android.content.Intent.ACTION_BOOT_COMPLETED, wasSharing = true))
    assertFalse(sharingAfter(null, wasSharing = true))
  }

  @Test
  fun sharingEndsWhenWifiGoesOffOnly() {
    assertTrue(wifiGoingOff(android.net.wifi.WifiManager.WIFI_STATE_DISABLING))
    assertTrue(wifiGoingOff(android.net.wifi.WifiManager.WIFI_STATE_DISABLED))
    assertFalse("the sticky state handed over at registration", wifiGoingOff(android.net.wifi.WifiManager.WIFI_STATE_ENABLED))
    assertFalse(wifiGoingOff(android.net.wifi.WifiManager.WIFI_STATE_ENABLING))
    assertFalse(wifiGoingOff(android.net.wifi.WifiManager.WIFI_STATE_UNKNOWN))
  }

  @Test
  fun quietHoursOverMidnightAndInTheDay() {
    val night = QuietHours(23 * 60, 7 * 60)
    val at = { h: Int, m: Int -> java.time.LocalDateTime.of(2026, 11, 2, h, m) }
    assertTrue(night.contains(at(23, 0)))
    assertTrue(night.contains(at(3, 30)))
    assertFalse("the end is outside", night.contains(at(7, 0)))
    assertFalse(night.contains(at(12, 0)))
    assertEquals("after midnight they began the evening before", java.time.LocalDate.of(2026, 11, 1), night.startOf(at(3, 0)))
    assertEquals(java.time.LocalDate.of(2026, 11, 2), night.startOf(at(23, 30)))
    val school = QuietHours(8 * 60, 15 * 60)
    assertTrue(school.contains(at(9, 0)))
    assertFalse(school.contains(at(16, 0)))
    assertFalse("start and end the same: never", QuietHours(600, 600).contains(at(10, 0)))
    assertEquals("23:00–07:00", night.label())
    assertEquals(night, QuietHours.parse(night.toString()))
    assertNull(QuietHours.parse("99999-1"))
  }

  @Test
  fun whyADeviceIsPaused() {
    val now = java.time.LocalDateTime.of(2026, 11, 2, 23, 30)
    val night = QuietHours(23 * 60, 7 * 60)
    assertEquals(Controls.Pause.Manual, pauseOf("manual", night, null, now))
    assertEquals(Controls.Pause.Limit, pauseOf("2026-11-02", null, null, now))
    assertEquals("yesterday's limit is over", Controls.Pause.None, pauseOf("2026-11-01", null, null, now))
    assertEquals("a lifted limit", Controls.Pause.None, pauseOf("free:2026-11-02", null, null, now))
    assertEquals(Controls.Pause.Quiet, pauseOf(null, night, null, now))
    assertEquals("tonight's quiet hours were lifted", Controls.Pause.None, pauseOf(null, night, "2026-11-02", now))
    assertEquals("yesterday's lifting does not count tonight", Controls.Pause.Quiet, pauseOf(null, night, "2026-11-01", now))
  }

  @Test
  fun monthlyPlanPeriods() {
    val d = { y: Int, m: Int, day: Int -> java.time.LocalDate.of(y, m, day) }
    assertEquals(d(2026, 11, 15), periodStart(d(2026, 11, 20), 15))
    assertEquals(d(2026, 10, 15), periodStart(d(2026, 11, 3), 15))
    assertEquals(d(2026, 12, 15), nextRenewal(d(2026, 11, 20), 15))
    assertEquals("the 31st in a short month is its last day", d(2027, 2, 28), nextRenewal(d(2027, 2, 1), 31))
    assertEquals(d(2027, 1, 31), periodStart(d(2027, 2, 10), 31))
    assertEquals(d(2026, 11, 1), periodStart(d(2026, 11, 1), 1))
    assertEquals(100L, perDayAllowance(1000, 10))
    assertEquals("used up", 0L, perDayAllowance(-5, 10))
    assertEquals(1000L, perDayAllowance(1000, 0))
  }

  @Test
  fun usageHistoryRoundTripsAndKeepsTwoMonths() {
    val days = mapOf(java.time.LocalDate.of(2026, 9, 1) to 5L, java.time.LocalDate.of(2026, 10, 31) to 123L, java.time.LocalDate.of(2026, 11, 1) to 0L)
    val text = historyText(days, java.time.LocalDate.of(2026, 9, 1))
    assertEquals("2026-10-31=123;2026-11-01=0", text)
    assertEquals(days - java.time.LocalDate.of(2026, 9, 1), parseHistory(text))
    assertEquals(emptyMap<java.time.LocalDate, Long>(), parseHistory("broken;=;2026-13-01=5"))
  }

  @Test
  fun batteryAndHeat() {
    assertEquals(BatteryAction.None, batteryAction(50, charging = false, warned = false, stopped = false))
    assertEquals(BatteryAction.Warn, batteryAction(15, charging = false, warned = false, stopped = false))
    assertEquals(BatteryAction.None, batteryAction(12, charging = false, warned = true, stopped = false))
    assertEquals(BatteryAction.Stop, batteryAction(5, charging = false, warned = true, stopped = false))
    assertEquals("started again after the stop", BatteryAction.None, batteryAction(4, charging = false, warned = true, stopped = true))
    assertEquals("charging", BatteryAction.None, batteryAction(3, charging = true, warned = false, stopped = false))
    assertFalse(hotNow(400, 0, wasHot = false))
    assertTrue(hotNow(455, 0, wasHot = false))
    assertTrue("cools only under 42 °C", hotNow(430, 0, wasHot = true))
    assertFalse(hotNow(415, 0, wasHot = true))
    assertTrue("the system says severe", hotNow(300, 3, wasHot = false))
  }

  @Test
  fun idleStopAndGuests() {
    assertFalse("never", idleOver(10_000_000, 0, 0))
    assertFalse(idleOver(29 * 60_000, 0, 30))
    assertTrue(idleOver(30 * 60_000, 0, 30))
    val mb = 1024L * 1024
    val g = Guests.Guest("Misafir", until = 3_600_000, limitMb = 500, used = 120 * mb)
    assertFalse(guestOver(g, 0))
    assertTrue("time is up", guestOver(g, 3_600_000))
    assertTrue("data is used up", guestOver(g.copy(used = 500 * mb), 0))
    assertFalse("no limits at all", guestOver(g.copy(until = 0, limitMb = 0, used = 99 * 1024 * mb), 99_999_999))
    assertEquals("Misafir · 45 dk kaldı · 120 MB / 500 MB", guestNote(g, 3_600_000 - 45 * 60_000))
    assertEquals("Misafir · 2 saat kaldı", guestNote(g.copy(until = 3 * 3_600_000, limitMb = 0), 3_600_000))
  }

  @Test
  fun thePhonesStoreFromItsUrl() {
    assertEquals("http://192.168.49.1:51823" to "abc123", storeOf("http://192.168.49.1:51823/hotspot.apk?t=abc123"))
    assertEquals("http://10.8.0.1:51823" to "x", storeOf("http://10.8.0.1:51823/phone?code=5&t=x"))
    assertNull("no token", storeOf("http://192.168.49.1:51823/hotspot.apk"))
    assertNull(storeOf("not a url"))
  }

  @Test
  fun searchesAfterALossWaitForTheSystemsDialog() {
    assertEquals("the first search", 60_000L, searchTimeout(first = true, patientLeft = 0))
    assertEquals("right after the network was lost", 60_000L, searchTimeout(first = false, patientLeft = 3))
    assertEquals("later: back to short", 12_000L, searchTimeout(first = false, patientLeft = 0))
  }

  @Test
  fun storeTokensAre32HexDigits() {
    assertTrue(validToken("0123456789abcdef0123456789abcdef"))
    assertFalse("too short", validToken("0123456789abcdef"))
    assertFalse("upper case is not what the app makes", validToken("0123456789ABCDEF0123456789ABCDEF"))
    assertFalse(validToken("0123456789abcdef0123456789abcdeg"))
  }

  @Test
  fun whereTheLinkIsSlow() {
    val r = SpeedTest.Result(downBps = 80_000_000, upBps = 20_000_000, pingMs = 70, bytes = 0, at = 0)
    assertNull("the phone itself: no Wi-Fi measured", bottleneck(r))
    assertEquals("Yavaşlık mobil internetten kaynaklanıyor; Wi-Fi daha hızlı.", bottleneck(r.copy(wifiDownBps = 700_000_000)))
    assertEquals("Yavaşlık Wi-Fi'den kaynaklanıyor. Telefona yaklaş ya da 5 GHz'e geç.", bottleneck(r.copy(wifiDownBps = 30_000_000)))
    assertEquals("Wi-Fi ile mobil internet aynı hızda.", bottleneck(r.copy(wifiDownBps = 85_000_000)))
  }

  @Test
  fun sharingNotificationTellsDevicesSpeedAndData() {
    val gib = 1024L * 1024 * 1024
    val n = shareNote(devices = 2, downBps = 525_000, upBps = 37_500, today = gib * 3 / 2, session = gib / 2)
    assertEquals("2 cihaz bağlı", n.title)
    assertEquals("↓ 4,2 Mbit/s   ↑ 300 kbit/s  ·  Bugün 1,5 GB", n.text)
    assertTrue(n.big!!.contains("bu oturumda 512 MB"))
    assertFalse("the mode is not named there", (n.title + n.text + n.big).contains("DPI"))
    val idle = shareNote(devices = 0, downBps = 0, upBps = 0, today = 0, session = 0)
    assertEquals("Paylaşım açık", idle.title)
    assertTrue(idle.big!!.contains("Cihazlar bağlanınca"))
  }

  @Test
  fun connectedNotificationTellsSpeedAndTotals() {
    val n = clientNote(downBps = 1_250_000, upBps = 125_000, received = 2048L * 1024 * 1024, sent = 80L * 1024 * 1024)
    assertEquals("Bağlanıldı", n.title)
    assertEquals("↓ 10,0 Mbit/s   ↑ 1,0 Mbit/s", n.text)
    assertTrue(n.big!!.contains("İndirilen 2,0 GB") && n.big!!.contains("Yüklenen 80 MB"))
    assertEquals(1000L, perSecond(3000, 1000, 2000))
    assertEquals("a counter that started again", 0L, perSecond(500, 1000, 2000))
    assertEquals("the first look", 0L, perSecond(500, null, 2000))
  }

  @Test
  fun dataAmountsAreWrittenTheTurkishWay() {
    assertEquals("512 B", humanBytes(512))
    assertEquals("850 KB", humanBytes(850L * 1024))
    assertEquals("120 MB", humanBytes(120L * 1024 * 1024))
    assertEquals("1,5 GB", humanBytes(1536L * 1024 * 1024))
    assertEquals("15,6 GB", humanBytes((15.6 * 1024 * 1024 * 1024).toLong()))
    assertEquals("250 GB", humanBytes(250L * 1024 * 1024 * 1024))
  }

  // The ending follows the way the number is read: kırk beşi, altısı, onu, kırkı, yüzü; yedide, otuzda, üçte.
  @Test
  fun endingsAfterNumbersFollowTheirSound() {
    val its = mapOf(0 to "ı", 1 to "i", 2 to "si", 3 to "ü", 4 to "ü", 5 to "i", 6 to "sı", 7 to "si", 8 to "i", 9 to "u",
      10 to "u", 20 to "si", 30 to "u", 40 to "ı", 45 to "i", 50 to "si", 60 to "ı", 70 to "i", 80 to "i", 90 to "ı", 100 to "ü")
    for ((n, ending) in its) assertEquals("%$n'$ending", Tr.share(n))
    assertEquals("07:00'de", Tr.atClock(7 * 60))
    assertEquals("07:30'da", Tr.atClock(7 * 60 + 30))
    assertEquals("03:00'te", Tr.atClock(3 * 60))
    assertEquals("06:00'da", Tr.atClock(6 * 60))
    assertEquals("22:40'ta", Tr.atClock(22 * 60 + 40))
    assertEquals("00:00'da", Tr.atClock(0))
    assertEquals("23:05'te", Tr.atClock(23 * 60 + 5))
  }

  // Releases up to 38 were called 1.0.N; 39, the first one published, is 1.1, 40 is 1.2, and the names count on.
  @Test
  fun releasesAreNamedByTheirNumber() {
    assertEquals("1.0.38", releaseName(38))
    assertEquals("1.1", releaseName(39))
    assertEquals("1.2", releaseName(40))
    assertEquals("1.2.1", releaseName(41))
    assertEquals("1.2.11", releaseName(51))
  }

  // update.json may only name files among this project's releases on GitHub.
  @Test
  fun onlineReleasesMustBeThisProjects() {
    val sum = "ab".repeat(32)
    val url = "https://github.com/${OnlineUpdate.REPO}/releases/download/v1.1.1/AleShare-1.1.1.apk"
    assertTrue(OnlineUpdate.valid(40, "1.1.1", url, 25_000_000, sum))
    assertFalse("another site", OnlineUpdate.valid(40, "1.1.1", "https://example.com/AleShare-1.1.1.apk", 25_000_000, sum))
    assertFalse("another repository", OnlineUpdate.valid(40, "1.1.1", url.replace(OnlineUpdate.REPO, "someone/AleShare"), 25_000_000, sum))
    assertFalse("no name", OnlineUpdate.valid(40, "", url, 25_000_000, sum))
    assertFalse("no size", OnlineUpdate.valid(40, "1.1.1", url, 0, sum))
    assertFalse("a short checksum", OnlineUpdate.valid(40, "1.1.1", url, 25_000_000, "abcd"))
  }

  @Test
  fun looksForReleasesAreSpaced() {
    val hour = 3_600_000L
    val t0 = 1_800_000_000_000L
    assertTrue("never looked", OnlineUpdate.due(t0, 0, 0))
    assertFalse(OnlineUpdate.due(t0 + 2 * hour, t0, t0))
    assertTrue("an answer holds for three hours", OnlineUpdate.due(t0 + 3 * hour + 1, t0, t0))
    assertFalse("no answer: not again within half an hour", OnlineUpdate.due(t0 + 4 * hour + 600_000, t0, t0 + 4 * hour))
    assertTrue(OnlineUpdate.due(t0 + 4 * hour + 31 * 60_000, t0, t0 + 4 * hour))
    assertTrue("a clock set back must not silence the looks", OnlineUpdate.due(t0 - hour, t0, t0))
  }

}
