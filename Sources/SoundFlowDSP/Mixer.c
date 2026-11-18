#include "SoundFlowDSP.h"
#include <stdatomic.h>
#include <stdlib.h>
#include <math.h>
#include <string.h>

#define CAPACITY 32768u
#define MASK (CAPACITY - 1)
typedef struct {
    _Atomic uint64_t write_pos, read_pos, underruns;
    _Atomic uint32_t flush;
    _Atomic float gain;
    float samples[CAPACITY * 2];
    // Consumer-only state below.
    double fraction;
    int primed;
    float last_l, last_r, current_gain;
} SFStream;
struct SFMixer {
    SFStream streams[SF_MAX_STREAMS];
    _Atomic float master;
    _Atomic uint32_t target;
};
SFMixer *sf_mixer_create(uint32_t target) {
    SFMixer *m = calloc(1, sizeof(SFMixer));
    if (!m) return NULL;
    atomic_init(&m->master, 1.f);
    atomic_init(&m->target, target);
    for (int i = 0; i < SF_MAX_STREAMS; i++) {
        atomic_init(&m->streams[i].write_pos, 0);
        atomic_init(&m->streams[i].read_pos, 0);
        atomic_init(&m->streams[i].underruns, 0);
        atomic_init(&m->streams[i].flush, 0);
        atomic_init(&m->streams[i].gain, 1.f);
    }
    return m;
}
void sf_mixer_destroy(SFMixer *m) { free(m); }
size_t sf_mixer_write_s16le(SFMixer *m, uint32_t slot, const uint8_t *pcm, size_t frames) {
    if (!m || slot >= SF_MAX_STREAMS || frames > CAPACITY) return 0;
    SFStream *s = &m->streams[slot];
    uint64_t w = atomic_load_explicit(&s->write_pos, memory_order_relaxed);
    uint64_t r = atomic_load_explicit(&s->read_pos, memory_order_acquire);
    if (w - r + frames > CAPACITY) return 0;
    for (size_t i = 0; i < frames; i++) {
        for (int c = 0; c < 2; c++) {
            size_t p = i * 4 + c * 2;
            int16_t sample = (int16_t)((uint16_t)pcm[p] | ((uint16_t)pcm[p + 1] << 8));
            s->samples[((w + i) & MASK) * 2 + c] = sample / 32768.f;
        }
    }
    atomic_store_explicit(&s->write_pos, w + frames, memory_order_release);
    return frames;
}
void sf_mixer_flush(SFMixer *m, uint32_t slot) {
    if (m && slot < SF_MAX_STREAMS) atomic_store(&m->streams[slot].flush, 1);
}
void sf_mixer_set_gain(SFMixer *m, uint32_t slot, float v) {
    if (m && slot < SF_MAX_STREAMS) atomic_store(&m->streams[slot].gain, fminf(2, fmaxf(0, v)));
}
void sf_mixer_set_master(SFMixer *m, float v) { if (m) atomic_store(&m->master, fminf(1, fmaxf(0, v))); }
void sf_mixer_set_target(SFMixer *m, uint32_t v) { if (m) atomic_store(&m->target, v < 960 ? 960 : v > 9600 ? 9600 : v); }
uint32_t sf_mixer_buffered(SFMixer *m, uint32_t slot) {
    if (!m || slot >= SF_MAX_STREAMS) return 0;
    SFStream *s = &m->streams[slot];
    uint64_t r = atomic_load_explicit(&s->read_pos, memory_order_acquire);
    uint64_t w = atomic_load_explicit(&s->write_pos, memory_order_acquire);
    return (uint32_t)(w >= r ? w - r : 0);
}
uint64_t sf_mixer_underruns(SFMixer *m, uint32_t slot) {
    return m && slot < SF_MAX_STREAMS ? atomic_load(&m->streams[slot].underruns) : 0;
}
static inline float clamp(float v) { return fminf(1.f, fmaxf(-1.f, v)); }
void sf_mixer_render(SFMixer *m, float *left, float *right, uint32_t frames) {
    memset(left, 0, frames * sizeof(float));
    memset(right, 0, frames * sizeof(float));
    if (!m) return;
    uint32_t target = atomic_load_explicit(&m->target, memory_order_relaxed);
    for (int slot = 0; slot < SF_MAX_STREAMS; slot++) {
        SFStream *s = &m->streams[slot];
        uint64_t r = atomic_load_explicit(&s->read_pos, memory_order_relaxed);
        uint64_t w = atomic_load_explicit(&s->write_pos, memory_order_acquire);
        if (atomic_exchange(&s->flush, 0)) {
            r = w; s->primed = 0; s->fraction = 0;
        }
        uint64_t available = w - r;
        if (available > target + 5760) { r = w - target; s->fraction = 0; available = target; }
        if (!s->primed && available >= target) s->primed = 1;
        // Gentle rate correction tracks independent sender and receiver clocks.
        double error = ((double)available - target) / (double)target;
        double ratio = 1.0 + fmin(.005, fmax(-.005, error * .002));
        float gain = atomic_load_explicit(&s->gain, memory_order_relaxed);
        int starved = 0;
        for (uint32_t i = 0; i < frames; i++) {
            s->current_gain += (gain - s->current_gain) * .01f;
            if (s->primed && r + 1 < w) {
                size_t a = (r & MASK) * 2, b = ((r + 1) & MASK) * 2;
                float t = (float)s->fraction;
                s->last_l = s->samples[a] + (s->samples[b] - s->samples[a]) * t;
                s->last_r = s->samples[a + 1] + (s->samples[b + 1] - s->samples[a + 1]) * t;
                s->fraction += ratio;
                uint64_t step = (uint64_t)s->fraction;
                r += step; s->fraction -= step;
            } else {
                if (s->primed) { starved = 1; s->primed = 0; s->fraction = 0; }
                s->last_l *= .97f; s->last_r *= .97f;
            }
            left[i] += s->last_l * s->current_gain;
            right[i] += s->last_r * s->current_gain;
        }
        if (starved) atomic_fetch_add_explicit(&s->underruns, 1, memory_order_relaxed);
        atomic_store_explicit(&s->read_pos, r, memory_order_release);
    }
    float master = atomic_load_explicit(&m->master, memory_order_relaxed);
    for (uint32_t i = 0; i < frames; i++) { left[i] = clamp(left[i] * master); right[i] = clamp(right[i] * master); }
}
