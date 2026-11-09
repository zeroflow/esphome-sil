import re

import esphome.codegen as cg
from esphome.components import light
import esphome.config_validation as cv
from esphome.const import (
    CONF_FROM,
    CONF_ID,
    CONF_NUM_LEDS,
    CONF_OUTPUT_ID,
    CONF_SEGMENTS,
    CONF_TO,
)
from esphome.core import CORE
import esphome.final_validate as fv

from . import CHILD_SCHEMA, register_child, sim_bus_ns

SimLight = sim_bus_ns.class_("SimLight", light.AddressableLight)
CONF_SEGMENT_LABELS = "segment_labels"

# A bus line is at most 4095 bytes; 255 LEDs are 510 hex characters of pixels.
MAX_LEDS = 255
LABEL_RE = re.compile(r"^[a-z][a-z0-9_]*$")


def validate_label(value):
    """Segment label as it appears in `sil status`: ^[a-z][a-z0-9_]*$."""
    value = cv.string(value)
    if not LABEL_RE.match(value):
        raise cv.Invalid(f"invalid segment label '{value}': must match ^[a-z][a-z0-9_]*$")
    return value


CONFIG_SCHEMA = (
    light.ADDRESSABLE_LIGHT_SCHEMA.extend(
        {
            cv.GenerateID(CONF_OUTPUT_ID): cv.declare_id(SimLight),
            cv.Required(CONF_NUM_LEDS): cv.int_range(min=1, max=MAX_LEDS),
            # partition light id -> label shown in `sil status` (default: the id minus a trailing `_led`)
            cv.Optional(CONF_SEGMENT_LABELS, default={}): cv.Schema({cv.string: validate_label}),
        }
    )
    .extend(CHILD_SCHEMA)
    .extend(cv.COMPONENT_SCHEMA)
)


def _ident(value):
    return getattr(value, "id", None) or str(value)


def partitions_of(light_config, full_config):
    """(partition id, from, to) of every stock partition light that has this light as a segment
    source, in the order of the config. The partition's own from/to are the single source of truth."""
    me = _ident(light_config[CONF_ID])
    out = []
    lights = full_config.get("light") or []
    if isinstance(lights, dict):
        lights = [lights]
    for entry in lights:
        if not isinstance(entry, dict) or entry.get("platform") != "partition":
            continue
        for seg in entry.get(CONF_SEGMENTS) or []:
            if seg.get(CONF_ID) is not None and _ident(seg[CONF_ID]) == me:
                out.append((_ident(entry[CONF_ID]), seg[CONF_FROM], seg[CONF_TO]))
    return out


def derive_segments(light_config, full_config):
    """`label:from:to,...` in partition config order (from and to inclusive). The label is the
    partition id minus a trailing `_led`, unless `segment_labels` names it."""
    labels = light_config.get(CONF_SEGMENT_LABELS) or {}
    out = []
    for pid, lo, hi in partitions_of(light_config, full_config):
        label = labels.get(pid)
        if label is None:
            label = pid[: -len("_led")] if pid.endswith("_led") else pid
        out.append(f"{label}:{lo}:{hi}")
    return ",".join(out)


def final_validate(config):
    """Reject a segment_labels key that names no partition of this light, and segment names that
    are not valid or not unique (the engine rejects the whole seg otherwise)."""
    full = fv.full_config.get()
    labels = config.get(CONF_SEGMENT_LABELS) or {}
    parts = partitions_of(config, full)
    pids = {p[0] for p in parts}
    for pid in labels:
        if pid not in pids:
            raise cv.Invalid(
                f"segment_labels names '{pid}', which is not a partition light of '{_ident(config[CONF_ID])}'"
            )
    seen = {}
    for part in derive_segments(config, full).split(","):
        if not part:
            continue
        label = part.split(":")[0]
        if not LABEL_RE.match(label):
            raise cv.Invalid(
                f"segment label '{label}' is not valid ([a-z][a-z0-9_]*); name it with segment_labels"
            )
        if label in seen:
            raise cv.Invalid(f"duplicate segment label '{label}'; name one of them with segment_labels")
        seen[label] = True
    return config


FINAL_VALIDATE_SCHEMA = final_validate


async def to_code(config):
    var = cg.new_Pvariable(config[CONF_OUTPUT_ID])
    await light.register_light(var, config)
    await cg.register_component(var, config)
    cg.add(var.set_num_leds(config[CONF_NUM_LEDS]))
    cg.add(var.set_segments(derive_segments(config, CORE.config)))
    await register_child(var, config)
