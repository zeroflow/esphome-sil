import re

import esphome.codegen as cg
import esphome.config_validation as cv
from esphome.const import CONF_ID, CONF_OUTPUT_ID, CONF_PATH
import esphome.final_validate as fv

CODEOWNERS = ["@zeroflow"]
sim_bus_ns = cg.esphome_ns.namespace("sim_bus")
SimBus = sim_bus_ns.class_("SimBus", cg.Component)
CONF_SIM_BUS_ID = "sim_bus_id"
CONF_CHANNEL = "channel"

# sockaddr_un.sun_path is 108 bytes including the terminating NUL.
SUN_PATH_LIMIT = 107
CHANNEL_RE = re.compile(r"^[a-z0-9_]+(\.[a-z0-9_]+)*$")


def validate_path(value):
    """Socket path of the hub: non-empty and at most 107 UTF-8 bytes (BUS-03)."""
    value = cv.string(value)
    if value == "":
        raise cv.Invalid("sim_bus path must not be empty")
    n = len(value.encode("utf-8"))
    if n > SUN_PATH_LIMIT:
        raise cv.Invalid(
            f"sim_bus path is {n} bytes; sun_path limit is 107."
        )
    return value


def validate_channel(value):
    """Bus channel name, e.g. port1.duty (IO-06)."""
    value = cv.string(value)
    if not CHANNEL_RE.match(value):
        raise cv.Invalid(
            f"invalid sim_bus channel '{value}': must match ^[a-z0-9_]+(\\.[a-z0-9_]+)*$"
        )
    return value


# The path default is only a fallback for hand-run firmware; sil and the e2e
# harness always set SIL_BUS, which wins at runtime.
CONFIG_SCHEMA = cv.Schema(
    {
        cv.GenerateID(): cv.declare_id(SimBus),
        cv.Optional(CONF_PATH, default="/tmp/sil.sock"): validate_path,
    }
).extend(cv.COMPONENT_SCHEMA)

CHILD_SCHEMA = cv.Schema(
    {
        cv.GenerateID(CONF_SIM_BUS_ID): cv.use_id(SimBus),
        cv.Required(CONF_CHANNEL): validate_channel,
    }
)


def check_unique_channels(config):
    """Reject two sim_bus platforms that share a channel (IO-06).

    Runs as the hub's final validation, i.e. once per config, with the full
    validated config available.
    """
    full = fv.full_config.get()
    seen = {}
    for domain in ("output", "sensor", "binary_sensor", "light"):
        entries = full.get(domain) or []
        if isinstance(entries, dict):
            entries = [entries]
        for entry in entries:
            if not isinstance(entry, dict) or entry.get("platform") != "sim_bus":
                continue
            channel = entry.get(CONF_CHANNEL)
            if channel is None:
                continue
            # A light is named by its output_id when it has one (the id of the underlying light).
            ident = entry.get(CONF_OUTPUT_ID) or entry.get(CONF_ID)
            ident = getattr(ident, "id", None) or str(ident)
            if channel in seen:
                raise cv.Invalid(
                    f"duplicate sim_bus channel '{channel}' used by {seen[channel]} and {ident}"
                )
            seen[channel] = ident
    return config


FINAL_VALIDATE_SCHEMA = check_unique_channels


async def to_code(config):
    var = cg.new_Pvariable(config[CONF_ID])
    await cg.register_component(var, config)
    cg.add(var.set_path(config[CONF_PATH]))


async def register_child(var, config):
    hub = await cg.get_variable(config[CONF_SIM_BUS_ID])
    cg.add(var.set_channel(config[CONF_CHANNEL]))
    cg.add(var.set_bus(hub))
    cg.add(hub.register_child(var))
