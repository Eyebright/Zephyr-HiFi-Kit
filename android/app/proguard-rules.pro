# The Go c-shared core is reached only through JNI; keep the native bridge class.
-keep class hk.zephyr.terminal.ZatCore { *; }