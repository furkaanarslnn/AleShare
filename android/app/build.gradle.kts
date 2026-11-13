plugins {
  alias(libs.plugins.android.application)
  alias(libs.plugins.compose.compiler)
}

// The release's number and name live in ../version.properties; build-android.ps1 -Release counts the number up and
// writes the name that belongs to it.
val releaseCode: Int =
  Regex("""(?m)^code=(\d+)""").find(rootProject.file("version.properties").readText())?.groupValues?.get(1)?.toInt()
    ?: error("version.properties has no code= line")
val releaseName: String =
  Regex("""(?m)^name=(\S+)""").find(rootProject.file("version.properties").readText())?.groupValues?.get(1)
    ?: error("version.properties has no name= line")

android {
  namespace = "dev.hotspot.app"
  compileSdk = 36
  defaultConfig {
    applicationId = "dev.hotspot.app"
    minSdk = 26
    targetSdk = 35
    versionCode = releaseCode
    versionName = releaseName
    // The Go core is built for arm64 only.
    ndk { abiFilters += "arm64-v8a" }
  }

  buildTypes {
    release {
      isMinifyEnabled = false
      proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
      // Signed with this computer's own debug key (~/.android/debug.keystore), so the release installs over the
      // builds made so far and keeps their pairings: Android keeps an app's data only across updates signed with
      // the same key. A store release would need a key of its own.
      signingConfig = signingConfigs.getByName("debug")
    }
  }
  compileOptions {
    sourceCompatibility = JavaVersion.VERSION_17
    targetCompatibility = JavaVersion.VERSION_17
  }
  buildFeatures {
    compose = true
    aidl = false
    buildConfig = false
    shaders = false
  }

  packaging {
    resources {
      excludes += "/META-INF/{AL2.0,LGPL2.1}"
    }
  }
}

kotlin {
  jvmToolchain(17)
}

dependencies {
  val composeBom = platform(libs.androidx.compose.bom)
  implementation(composeBom)

  implementation(libs.androidx.core.ktx)
  implementation(libs.androidx.lifecycle.runtime.ktx)
  implementation(libs.androidx.activity.compose)
  implementation(libs.androidx.lifecycle.runtime.compose)

  implementation(libs.androidx.compose.ui)
  implementation(libs.androidx.compose.ui.tooling.preview)
  implementation(libs.androidx.compose.material3)
  debugImplementation(libs.androidx.compose.ui.tooling)

  // QR code for pairing
  implementation(libs.zxing.core)
  // Scanning a QR code with the system's scanner: no camera permission of our own.
  implementation(libs.play.services.code.scanner)

  // The Go core (WireGuard server + user-space gateway), built by `gomobile bind` in ../core.
  implementation(files("libs/wgcore.aar"))

  // Unit tests of the app's own logic on the computer (build-android.ps1 -Release runs them).
  testImplementation(libs.junit)
}
