import java.util.Properties

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.kotlin.serialization)
}

// Release signing (#2): a gitignored keystore.properties at the repo root
// (storeFile, storePassword, keyAlias, keyPassword) or the MAMA_KEYSTORE_PATH,
// MAMA_KEYSTORE_PASSWORD, MAMA_KEY_ALIAS and MAMA_KEY_PASSWORD env vars.
// Without them the release build fails; it never falls back to debug signing.
val releaseSigning: Map<String, String>? = run {
    val propsFile = rootProject.file("keystore.properties")
    val values = if (propsFile.isFile) {
        val props = Properties().apply { propsFile.inputStream().use { load(it) } }
        listOf("storeFile", "storePassword", "keyAlias", "keyPassword").associateWith { props.getProperty(it).orEmpty() }
    } else {
        mapOf(
            "storeFile" to providers.environmentVariable("MAMA_KEYSTORE_PATH").orNull.orEmpty(),
            "storePassword" to providers.environmentVariable("MAMA_KEYSTORE_PASSWORD").orNull.orEmpty(),
            "keyAlias" to providers.environmentVariable("MAMA_KEY_ALIAS").orNull.orEmpty(),
            "keyPassword" to providers.environmentVariable("MAMA_KEY_PASSWORD").orNull.orEmpty(),
        )
    }
    values.takeIf { it.values.all(String::isNotBlank) }
}
val missingReleaseSigning: String? = when {
    releaseSigning == null ->
        "Release signing is not configured. Create keystore.properties at the repo root " +
            "(storeFile, storePassword, keyAlias, keyPassword) or set MAMA_KEYSTORE_PATH, " +
            "MAMA_KEYSTORE_PASSWORD, MAMA_KEY_ALIAS and MAMA_KEY_PASSWORD. See README, \"Signed release APK\"."
    !rootProject.file(releaseSigning.getValue("storeFile")).isFile ->
        "Release keystore not found at ${rootProject.file(releaseSigning.getValue("storeFile"))}."
    else -> null
}

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
    }

    signingConfigs {
        if (missingReleaseSigning == null && releaseSigning != null) {
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
    }
}

dependencies {
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.lifecycle.runtime.ktx)
    implementation(libs.androidx.activity.compose)
    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.compose.ui)
    implementation(libs.androidx.compose.ui.graphics)
    implementation(libs.androidx.compose.ui.tooling.preview)
    implementation(libs.androidx.compose.material3)
    implementation(libs.androidx.navigation.compose)
    implementation(libs.kotlinx.serialization.json)
    debugImplementation(libs.androidx.compose.ui.tooling)

    testImplementation(libs.junit)
}

// Fail any release build without signing credentials instead of shipping an unsigned APK.
gradle.taskGraph.whenReady {
    val wantsRelease = allTasks.any { it.project.path == project.path && it.name.contains("Release") }
    if (wantsRelease && missingReleaseSigning != null) {
        throw GradleException(missingReleaseSigning)
    }
}
