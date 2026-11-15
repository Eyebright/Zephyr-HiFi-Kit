/* JNI bridge between the Kotlin shell (hk.zephyr.terminal.ZatCore) and the Go
 * c-shared core (libzatagent.so). The Go exports are declared here directly to
 * match the cgo-generated C signatures; the shim converts jstring/jlong/jint and
 * frees every returned Go string with ZatFree. Built by CMake into libzatjni.so. */
#include <jni.h>
#include <stdlib.h>

extern char *ZatStart(char *cfgJSON);
extern void ZatStop(void);
extern char *ZatState(void);
extern char *ZatPause(void);
extern char *ZatStopPlayback(void);
extern char *ZatSeek(unsigned long long pts);
extern char *ZatVolume(int v);
extern void ZatFree(char *p);

static jstring take(JNIEnv *env, char *res) {
    jstring out = (*env)->NewStringUTF(env, res ? res : "");
    if (res) ZatFree(res);
    return out;
}

JNIEXPORT jstring JNICALL
Java_hk_zephyr_terminal_ZatCore_nativeStart(JNIEnv *env, jobject thiz, jstring cfg) {
    const char *c = (*env)->GetStringUTFChars(env, cfg, NULL);
    char *res = ZatStart((char *)c);
    (*env)->ReleaseStringUTFChars(env, cfg, c);
    return take(env, res);
}

JNIEXPORT void JNICALL
Java_hk_zephyr_terminal_ZatCore_nativeStop(JNIEnv *env, jobject thiz) {
    ZatStop();
}

JNIEXPORT jstring JNICALL
Java_hk_zephyr_terminal_ZatCore_nativeState(JNIEnv *env, jobject thiz) {
    return take(env, ZatState());
}

JNIEXPORT jstring JNICALL
Java_hk_zephyr_terminal_ZatCore_nativePause(JNIEnv *env, jobject thiz) {
    return take(env, ZatPause());
}

JNIEXPORT jstring JNICALL
Java_hk_zephyr_terminal_ZatCore_nativeStopPlayback(JNIEnv *env, jobject thiz) {
    return take(env, ZatStopPlayback());
}

JNIEXPORT jstring JNICALL
Java_hk_zephyr_terminal_ZatCore_nativeSeek(JNIEnv *env, jobject thiz, jlong pts) {
    return take(env, ZatSeek((unsigned long long)pts));
}

JNIEXPORT jstring JNICALL
Java_hk_zephyr_terminal_ZatCore_nativeVolume(JNIEnv *env, jobject thiz, jint v) {
    return take(env, ZatVolume((int)v));
}