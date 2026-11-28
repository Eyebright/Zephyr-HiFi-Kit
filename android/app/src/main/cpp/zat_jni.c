/* JNI bridge between the Kotlin shell (hk.zephyr.terminal.ZatCore) and the Go
 * c-shared core (libzatagent.so). The Go exports are declared here directly to
 * match the cgo-generated C signatures; the shim converts jstring/jlong/jint and
 * frees every returned Go string with ZatFree. Built by CMake into libzatjni.so. */
#include <jni.h>
#include <stdlib.h>

extern char *ZatStart(char *cfgJSON);
extern void ZatStop(void);
extern char *ZatDiscoverServer(void);
extern char *ZatState(void);
extern char *ZatPause(void);
extern char *ZatQueueNext(void);
extern char *ZatQueuePrev(void);
extern char *ZatStopPlayback(void);
extern char *ZatSeek(unsigned long long pts);
extern char *ZatVolume(int v);
extern char *ZatUsbProbe(char *devHex, char *cfgHex, char *product);
extern char *ZatUsbRates(char *rangeHex);
extern char *ZatUsbPlan(char *devHex, char *cfgHex, char *obsJSON);
extern char *ZatUsbValidate(int iface, int alt, char *replyHex);
extern char *ZatUsbToneStart(char *reqJSON);
extern char *ZatUsbToneStat(void);
extern char *ZatUsbToneStop(void);
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
Java_hk_zephyr_terminal_ZatCore_nativeDiscoverServer(JNIEnv *env, jobject thiz) {
    return take(env, ZatDiscoverServer());
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
Java_hk_zephyr_terminal_ZatCore_nativeQueueNext(JNIEnv *env, jobject thiz) {
    return take(env, ZatQueueNext());
}

JNIEXPORT jstring JNICALL
Java_hk_zephyr_terminal_ZatCore_nativeQueuePrev(JNIEnv *env, jobject thiz) {
    return take(env, ZatQueuePrev());
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

JNIEXPORT jstring JNICALL
Java_hk_zephyr_terminal_ZatCore_nativeUsbProbe(JNIEnv *env, jobject thiz, jstring devHex,
                                               jstring cfgHex, jstring product) {
    const char *d = (*env)->GetStringUTFChars(env, devHex, NULL);
    const char *c = (*env)->GetStringUTFChars(env, cfgHex, NULL);
    const char *p = (*env)->GetStringUTFChars(env, product, NULL);
    char *res = ZatUsbProbe((char *)d, (char *)c, (char *)p);
    (*env)->ReleaseStringUTFChars(env, devHex, d);
    (*env)->ReleaseStringUTFChars(env, cfgHex, c);
    (*env)->ReleaseStringUTFChars(env, product, p);
    return take(env, res);
}

JNIEXPORT jstring JNICALL
Java_hk_zephyr_terminal_ZatCore_nativeUsbRates(JNIEnv *env, jobject thiz, jstring rangeHex) {
    const char *r = (*env)->GetStringUTFChars(env, rangeHex, NULL);
    char *res = ZatUsbRates((char *)r);
    (*env)->ReleaseStringUTFChars(env, rangeHex, r);
    return take(env, res);
}

JNIEXPORT jstring JNICALL
Java_hk_zephyr_terminal_ZatCore_nativeUsbPlan(JNIEnv *env, jobject thiz, jstring devHex,
                                              jstring cfgHex, jstring obsJSON) {
    const char *d = (*env)->GetStringUTFChars(env, devHex, NULL);
    const char *c = (*env)->GetStringUTFChars(env, cfgHex, NULL);
    const char *o = (*env)->GetStringUTFChars(env, obsJSON, NULL);
    char *res = ZatUsbPlan((char *)d, (char *)c, (char *)o);
    (*env)->ReleaseStringUTFChars(env, devHex, d);
    (*env)->ReleaseStringUTFChars(env, cfgHex, c);
    (*env)->ReleaseStringUTFChars(env, obsJSON, o);
    return take(env, res);
}

JNIEXPORT jstring JNICALL
Java_hk_zephyr_terminal_ZatCore_nativeUsbValidate(JNIEnv *env, jobject thiz, jint iface, jint alt,
                                                  jstring replyHex) {
    const char *r = (*env)->GetStringUTFChars(env, replyHex, NULL);
    char *res = ZatUsbValidate((int)iface, (int)alt, (char *)r);
    (*env)->ReleaseStringUTFChars(env, replyHex, r);
    return take(env, res);
}

JNIEXPORT jstring JNICALL
Java_hk_zephyr_terminal_ZatCore_nativeUsbToneStart(JNIEnv *env, jobject thiz, jstring reqJSON) {
    const char *q = (*env)->GetStringUTFChars(env, reqJSON, NULL);
    char *res = ZatUsbToneStart((char *)q);
    (*env)->ReleaseStringUTFChars(env, reqJSON, q);
    return take(env, res);
}

JNIEXPORT jstring JNICALL
Java_hk_zephyr_terminal_ZatCore_nativeUsbToneStat(JNIEnv *env, jobject thiz) {
    return take(env, ZatUsbToneStat());
}

JNIEXPORT jstring JNICALL
Java_hk_zephyr_terminal_ZatCore_nativeUsbToneStop(JNIEnv *env, jobject thiz) {
    return take(env, ZatUsbToneStop());
}
