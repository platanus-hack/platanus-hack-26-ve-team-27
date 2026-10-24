import java.util.Properties

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.kotlin.serialization)
}

// Release signing (#2): the MAMA_KEYSTORE_PATH, MAMA_KEYSTORE_PASSWORD, MAMA_KEY_ALIAS
// and MAMA_KEY_PASSWORD env vars (when all four are set), otherwise a gitignored
// keystore.properties at the repo root (storeFile, storePassword, keyAlias, keyPassword).
// Without them the release build fails; it never falls back to debug signing.
val signingKeys = listOf("storeFile", "storePassword", "keyAlias", "keyPassword")
val signingEnv = mapOf(
    "storeFile" to "MAMA_KEYSTORE_PATH",
    "storePassword" to "MAMA_KEYSTORE_PASSWORD",
    "keyAlias" to "MAMA_KEY_ALIAS",
    "keyPassword" to "MAMA_KEY_PASSWORD",
)
val releaseSigningSource: Pair<String, Map<String, String>> = run {
    val fromEnv = signingKeys.associateWith { providers.environmentVariable(signingEnv.getValue(it)).orNull.orEmpty().trim() }
    val propsFile = rootProject.file("keystore.properties")
    when {
        fromEnv.values.all(String::isNotBlank) || !propsFile.isFile -> "env vars" to fromEnv
        else -> {
            val props = Properties().apply { propsFile.inputStream().use { load(it) } }
            "keystore.properties" to signingKeys.associateWith { props.getProperty(it).orEmpty().trim() }
        }
    }
}
val releaseSigning: Map<String, String> = releaseSigningSource.second
val missingReleaseSigning: String? = run {
    val (source, values) = releaseSigningSource
    val blank = values.filterValues(String::isBlank).keys
    when {
        blank.isNotEmpty() -> {
            val missing = if (source == "env vars") blank.map(signingEnv::getValue) else blank
            "Release signing is not configured (missing in $source: ${missing.joinToString()}). " +
                "Set MAMA_KEYSTORE_PATH, MAMA_KEYSTORE_PASSWORD, MAMA_KEY_ALIAS and MAMA_KEY_PASSWORD, " +
                "or create keystore.properties at the repo root (storeFile, storePassword, keyAlias, keyPassword). " +
                "See README, \"Signed release APK\"."
        }
        !rootProject.file(values.getValue("storeFile")).isFile ->
            "Release keystore not found at ${rootProject.file(values.getValue("storeFile"))}."
        else -> null
    }
}

// First-run setup (#15): where the app sends DeviceRegistration and the number it
// saves as the "Mamá" contact. Each comes from a Gradle property (-Pmama.serverUrl=...,
// or ~/.gradle/gradle.properties), else an env var. Neither is a secret, but both
// change per deploy, so they are not hard-coded. The server URL defaults to
// http://127.0.0.1:8080, which reaches a laptop's local server after
// `adb reverse tcp:8080 tcp:8080`.
fun buildSetting(property: String, vararg envVars: String, default: String = ""): String =
    (listOf(providers.gradleProperty(property)) + envVars.map { providers.environmentVariable(it) })
        .firstNotNullOfOrNull { it.orNull?.trim()?.takeIf(String::isNotEmpty) } ?: default
val mamaServerUrl = buildSetting("mama.serverUrl", "MAMA_SERVER_URL", default = "http://127.0.0.1:8080")
val mamaTwilioNumber = buildSetting("mama.twilioNumber", "MAMA_TWILIO_NUMBER", "TWILIO_PHONE_NUMBER")
// Fallback SMS number for the offline path until GET /config has been cached (#55).
// Obviously fake placeholder (NANP 555-01xx); real deployments override it.
val mamaDefaultSmsNumber = buildSetting("mama.defaultSmsNumber", "MAMA_DEFAULT_SMS_NUMBER", default = "+15555550100")
// The offline emergency channel (#55) is an extra, off unless asked for: "true" or "1" turns it on.
val mamaOfflineEmergency = buildSetting("mama.offlineEmergency", "MAMA_OFFLINE_EMERGENCY").let { it == "1" || it.equals("true", ignoreCase = true) }
fun String.asBuildConfigString(): String = "\"" + replace("\\", "\\\\").replace("\"", "\\\"") + "\""

android {
    namespace = "com.team27.mama"
    compileSdk {
        version = release(37)
    }

    defaultConfig {
        applicationId = "com.team27.mama"
        minSdk = 26
        targetSdk = 36
        versionCode = 1
        versionName = "0.1.0"

        buildConfigField("String", "SERVER_URL", mamaServerUrl.asBuildConfigString())
        buildConfigField("String", "TWILIO_NUMBER", mamaTwilioNumber.asBuildConfigString())
        buildConfigField("String", "DEFAULT_SMS_NUMBER", mamaDefaultSmsNumber.asBuildConfigString())
        buildConfigField("boolean", "OFFLINE_EMERGENCY_ENABLED", mamaOfflineEmergency.toString())
    }

    signingConfigs {
        if (missingReleaseSigning == null) {
            create("release") {
                storeFile = rootProject.file(releaseSigning.getValue("storeFile"))
                storePassword = releaseSigning.getValue("storePassword")
                keyAlias = releaseSigning.getValue("keyAlias")
                keyPassword = releaseSigning.getValue("keyPassword")
            }
        }
    }

    buildTypes {
        release {
            signingConfig = signingConfigs.findByName("release")
            isMinifyEnabled = false
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    buildFeatures {
        compose = true
        buildConfig = true
    }
}

// Gradle does not track files a unit test reads at runtime. Declare the shared vector and
// sample directories as inputs so editing them re-runs the tests instead of hitting the cache.
tasks.withType<Test>().configureEach {
    inputs.dir(rootProject.file("server/internal/emergency/payload/testdata"))
        .withPropertyName("payloadVectors")
        .withPathSensitivity(PathSensitivity.RELATIVE)
    inputs.dir(rootProject.file("server/internal/contract/testdata"))
        .withPropertyName("contractSamples")
        .withPathSensitivity(PathSensitivity.RELATIVE)
}

dependencies {
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.lifecycle.runtime.ktx)
    implementation(libs.androidx.lifecycle.runtime.compose)
    implementation(libs.androidx.lifecycle.viewmodel.compose)
    implementation(libs.androidx.activity.compose)
    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.compose.ui)
    implementation(libs.androidx.compose.ui.graphics)
    implementation(libs.androidx.compose.ui.tooling.preview)
    implementation(libs.androidx.compose.material3)
    implementation(libs.androidx.navigation.compose)
    implementation(libs.kotlinx.serialization.json)
    implementation(libs.bouncycastle.bcprov)
    implementation(libs.kotlinx.coroutines.android)
    debugImplementation(libs.androidx.compose.ui.tooling)

    testImplementation(libs.junit)
}

// Fail any build that packages or installs a release artifact without signing credentials,
// instead of shipping an unsigned APK. Release unit tests and compiles still run without them.
val releaseArtifactTask = Regex("^(assemble|bundle|package|install|sign).*Release.*")
gradle.taskGraph.whenReady {
    val wantsRelease = allTasks.any { it.project.path == project.path && releaseArtifactTask.matches(it.name) }
    if (wantsRelease && missingReleaseSigning != null) {
        throw GradleException(missingReleaseSigning)
    }
}
