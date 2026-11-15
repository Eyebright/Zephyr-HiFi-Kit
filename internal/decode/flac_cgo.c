//go:build cgo && linux

#include "_cgo_export.h"
#include <FLAC/stream_decoder.h>
#include <stdio.h>

/* Trampolines bridging libFLAC's C callbacks to the exported Go functions.
   const is cast away to match the CGO-generated export signatures. */
static FLAC__StreamDecoderWriteStatus zat_wcb(const FLAC__StreamDecoder *d,
                                              const FLAC__Frame *f,
                                              const FLAC__int32 *const b[],
                                              void *cd) {
    return goFLACWrite((FLAC__StreamDecoder *)d, (FLAC__Frame *)f,
                       (FLAC__int32 **)b, cd);
}

static void zat_mcb(const FLAC__StreamDecoder *d, const FLAC__StreamMetadata *m,
                    void *cd) {
    goFLACMeta((FLAC__StreamDecoder *)d, (FLAC__StreamMetadata *)m, cd);
}

static void zat_ecb(const FLAC__StreamDecoder *d,
                    FLAC__StreamDecoderErrorStatus s, void *cd) {
    goFLACErr((FLAC__StreamDecoder *)d, s, cd);
}

FLAC__StreamDecoder *zatFLACNew(void) { return FLAC__stream_decoder_new(); }

/* init_file (filename-based) opens and owns the FILE internally; libFLAC closes
   it on finish/delete, so the Go side must never fopen/fclose it itself. */
FLAC__StreamDecoderInitStatus zatFLACInitFile(FLAC__StreamDecoder *d,
                                              const char *path, void *cd) {
    return FLAC__stream_decoder_init_file(d, path, zat_wcb, zat_mcb, zat_ecb, cd);
}