cmake_minimum_required(VERSION 3.22.1)
project(zatjni C)

# The Go c-shared core (libzatagent.so) is cross-built by scripts/build-android.sh
# and placed in jniLibs/<abi>/ before Gradle runs. Import it so the JNI shim links
# against the Zat* exports; Gradle packages the same .so into the APK.
# IMPORTED_SONAME makes libzatjni.so record the bare basename "libzatagent.so" in
# DT_NEEDED; without it the linker embeds this absolute build path, which does not
# exist on the device and crashes System.loadLibrary. The .so is also built with a
# matching -soname (see build-android.sh) so the basename is recorded regardless.
set(GO_LIB_DIR "${CMAKE_CURRENT_SOURCE_DIR}/../jniLibs/${ANDROID_ABI}")
add_library(zatagent SHARED IMPORTED)
set_target_properties(zatagent PROPERTIES
  IMPORTED_LOCATION "${GO_LIB_DIR}/libzatagent.so"
  IMPORTED_SONAME "libzatagent.so")

add_library(zatjni SHARED zat_jni.c)
target_link_libraries(zatjni zatagent log)