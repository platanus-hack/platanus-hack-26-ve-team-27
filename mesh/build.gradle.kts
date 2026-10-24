// BLE mesh library: bitchat-android code (GPL-3.0), see NOTICE.md and docs/adr/0002-bitchat-mesh-module.md.
plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.parcelize)
}

android {
    namespace = "com.team27.mama.mesh"
    compileSdk {
        version = release(37)
    }

    defaultConfig {
        minSdk = 26
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    testOptions {
        // The copied bitchat code calls android.util.Log, which throws on the plain JVM otherwise.
        unitTests.isReturnDefaultValues = true
    }
}

dependencies {
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.lifecycle.process)
    api(libs.kotlinx.coroutines.android) // Flow is part of the public MeshGateway API
    implementation(libs.bouncycastle.bcprov)
    implementation(libs.androidx.security.crypto)

    testImplementation(libs.junit)
}
