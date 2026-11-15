cmake_minimum_required(VERSION 3.22.1)
project(zatjni C)

# The Go c-shared core (libzatagent.so) is cross-built by scripts/build-android.sh
# and placed in jniLibs/<abi>/ before Gradle runs. Import it so the JNI shim links
# against the Zat* exports; Gradle packages the same .so into the APK.
set(GO_LIB_DIR "${CMAKE_CURRENT_SOURCE_DIR}/../jniLibs/${ANDROID_ABI}")
add_library(zatagent SHARED IMPORTED)
set_target_properties(zatagent PROPERTIES IMPORTED_LOCATION "${GO_LIB_DIR}/libzatagent.so")

add_library(zatjni SHARED zat_jni.c)
target_link_libraries(zatjni zatagent log)