import json
import math
import os
import shutil
import subprocess

import bpy
import bpy_extras
from bpy.props import BoolProperty, IntProperty, StringProperty
from bpy_extras.io_utils import ExportHelper
from mathutils import Matrix, Vector

bl_info = {
    "name": "Export Overdrive scenes format",
    "author": "Zephyr",
    "version": (0, 1),
    "blender": (4, 0, 0),
    "location": "File > Export > Overdrive scene [toml]",
    "description": "Export scene using Overdrive format [toml]",
    "warning": "",
    "wiki_url": "",
    "tracker_url": "",
    "category": "Import-Export"}


class OverdriveWriter:

    # Formats the engine decodes as they are; anything else becomes a PNG
    ENGINE_FORMATS = {'.png', '.jpg', '.jpeg'}
    # Every MTL key that names a texture, the file being the line's last field
    MAP_KEYS = {'map_Kd', 'map_Ks', 'map_Ns', 'map_d', 'map_Bump', 'bump', 'norm',
                'map_Pr', 'map_Pm', 'map_Ps', 'map_Ke', 'refl', 'map_refl', 'disp'}

    # Bakes the environment's prefiltered specular levels and irradiance as equirect
    # .hdr files: unmirrored to match the engine's mapping, a 256 cube face for a
    # 512x256 level 0, and levels down to 8 for scene/ibl.go's six
    CMGEN_FLAGS = ['--quiet', '--no-mirror', '--type=equirect', '--format=hdr',
                   '--size=256', '--ibl-min-lod-size=8']

    # Where install_cmgen.sh puts it, which a sandboxed Blender's PATH leaves out:
    # the flatpak's is only /app/bin:/usr/bin
    INSTALLED_CMGEN = os.path.expanduser('~/.local/bin/cmgen')

    def __init__(self, context, filepath, cmgen='cmgen'):
        self.context = context
        self.filepath = filepath
        self.working_dir = os.path.dirname(self.filepath)
        self.textures_dir = os.path.join(self.working_dir, 'textures')
        self.cmgen = cmgen
        if not shutil.which(cmgen) and shutil.which(self.INSTALLED_CMGEN):
            self.cmgen = self.INSTALLED_CMGEN
        # Source path to exported name, so a texture shared by meshes is converted once
        self.exported_textures = {}

    def write(self):
        """Writes the Blender scene as an Overdrive TOML scene file"""
        self.data = {'mesh': [], 'light': []}

        # 1) export one camera
        cameras = [cam for cam in self.context.scene.objects
                   if cam.type in {'CAMERA'}]
        if(len(cameras) == 0):
            print("WARN: No camera to export")
        else:
            if(len(cameras) > 1):
                print("WARN: Does not handle multiple camera, only export the active one")
            self.data['camera'] = self.write_camera(self.context.scene.camera)  # export the active one

        # 2) export all meshes
        if not os.path.exists(self.working_dir + "/meshes"):
            os.makedirs(self.working_dir + "/meshes")

        meshes = [obj for obj in self.context.scene.objects
                  if obj.type in {'MESH', 'FONT', 'SURFACE', 'META'}]
        print(meshes)
        for mesh in meshes:
            self.write_mesh(mesh)

        # 3) export all lights
        lights = [obj for obj in self.context.scene.objects
                  if obj.type in {'LIGHT'}]
        for light in lights:
            self.write_light(light)

        # 4) export the world's environment texture
        self.write_environment()

        # 5) write the scene file
        with open(self.filepath, "w") as f:
            f.write(self.write_toml())

    def write_toml(self):
        """Formats self.data: the single tables first, as TOML requires them
        before any [[array]] entry, then one block per mesh and light"""
        lines = ["# Written by the Overdrive Blender add-on: positions in Blender's z-up",
                 "# axes, angles in degrees", ""]
        for key in ('camera', 'environment'):
            if key in self.data:
                lines.append(f"[{key}]")
                lines += [f"{k} = {self.toml_value(v)}" for k, v in self.data[key].items()]
                lines.append("")
        for key in ('mesh', 'light'):
            for entry in self.data[key]:
                lines.append(f"[[{key}]]")
                lines += [f"{k} = {self.toml_value(v)}" for k, v in entry.items()]
                lines.append("")
        return "\n".join(lines)

    def toml_value(self, value):
        if isinstance(value, bool):
            return "true" if value else "false"
        if isinstance(value, str):
            # TOML's basic strings take JSON's escapes
            return json.dumps(value)
        if isinstance(value, (int, float)):
            # 6 decimals is past float32 precision at scene scale, and drops
            # Blender's 1e-17 noise; adding 0.0 turns -0.0 into 0.0
            return repr(round(float(value), 6) + 0.0)
        return "[" + ", ".join(self.toml_value(v) for v in value) + "]"

    def degrees(self, radians):
        # 4 decimals, or float32 noise shows: Blender's 30 degrees reads 29.999997
        return round(math.degrees(radians), 4)

    def write_camera(self, cam):
        """The active camera as the [camera] table"""
        return {
            "name": cam.name,
            "type": cam.data.type.lower(),
            "position": cam.location,
            "front": cam.matrix_world.to_quaternion() @ Vector((0, 0, -1)),
            "up": cam.matrix_world.to_quaternion() @ Vector((0, 1, 0)),
            "yaw": self.degrees(cam.rotation_euler[2]),
            "pitch": self.degrees(cam.rotation_euler[0]),
            "fov": self.degrees(cam.data.angle),
        }

    def write_mesh(self, mesh):
        viewport_selection = self.context.selected_objects
        bpy.ops.object.select_all(action='DESELECT')

        obj_name = mesh.name + ".obj"
        mtl_name = mesh.name + ".mtl"
        obj_path = os.path.join(self.working_dir, 'meshes', obj_name)
        mesh.select_set(True)
        # The OBJ holds the mesh about its own origin and the TOML its transform,
        # so the engine knows the rotation and scale rather than finding them
        # baked into the vertices. World, not parent-relative, as the engine has
        # no hierarchy: decomposing matrix_world covers a parented object too
        world = mesh.matrix_world.copy()
        # A mirror stays in the OBJ, which Blender writes with its faces turned
        # back out, so the transform the engine builds never flips winding
        mirror = Matrix.Diagonal((1, 1, -1, 1)) if world.determinant() < 0 else Matrix.Identity(4)
        mesh.matrix_world = mirror
        self.context.view_layer.update()
        try:
            # PBR extensions write Pr/Pm and map_Pr, which the engine reads as
            # roughness and metallic; without them only Kd survives
            bpy.ops.wm.obj_export(filepath=obj_path, check_existing=False,
                                  export_selected_objects=True, export_smooth_groups=False,
                                  export_materials=True, export_triangulated_mesh=True,
                                  export_pbr_extensions=True, apply_modifiers=True)
        finally:
            mesh.matrix_world = world
            self.context.view_layer.update()
        mesh.select_set(False)
        self.warn_grouped_materials(mesh)
        self.localise_mtl(os.path.join(self.working_dir, 'meshes', mtl_name))

        # The mirror undone, so what is left has a positive scale on every axis
        location, rotation, scale = (world @ mirror).decompose()
        entry = {
            "name": mesh.name,
            "position": location,
            "rotation": [self.degrees(a) for a in rotation.to_euler('XYZ')],
            "scale": scale,
            "obj": obj_name,
            "mtl": mtl_name,
        }
        # Blender's own "Shadow" ray visibility. Written only when it is off, the
        # engine defaulting to true, so existing scenes keep meaning what they did
        if not mesh.visible_shadow:
            entry["castsShadow"] = False
        self.data['mesh'].append(entry)

        for ob in viewport_selection:
            ob.select_set(True)

    def warn_grouped_materials(self, obj):
        """Warns of a material whose surface comes out of a node group: Blender's
        OBJ exporter reads only a Principled BSDF wired straight to the output,
        so such a material exports with no maps (Poly Haven's foliage)"""
        for slot in obj.material_slots:
            material = slot.material
            if not material or not material.use_nodes:
                continue
            for node in material.node_tree.nodes:
                if node.type == 'OUTPUT_MATERIAL' and node.is_active_output:
                    links = node.inputs['Surface'].links
                    if links and links[0].from_node.type == 'GROUP':
                        print(f"WARN: material {material.name} on {obj.name} is built in a "
                              f"node group, so its maps are not exported: ungroup it")

    def localise_mtl(self, mtl_path):
        """Copies or converts every texture an MTL names into textures/, and
        rewrites its line to the bare file name the engine resolves there"""
        if not os.path.exists(mtl_path):
            return
        with open(mtl_path) as f:
            lines = f.read().splitlines()
        out = []
        for line in lines:
            fields = line.split()
            if fields and fields[0] in self.MAP_KEYS and len(fields) > 1:
                # Colour maps stay sRGB; every other map is data and must not be
                # gamma-encoded on its way to 8 bits
                name = self.export_texture(fields[-1], is_data=fields[0] != 'map_Kd')
                if name:
                    line = ' '.join(fields[:-1] + [name])
            out.append(line)
        with open(mtl_path, 'w') as f:
            f.write('\n'.join(out) + '\n')

    def export_texture(self, ref, is_data):
        """Puts one texture in textures/, as a PNG unless the engine reads its format"""
        src = bpy.path.abspath(ref)
        if src in self.exported_textures:
            return self.exported_textures[src]
        if not os.path.exists(src):
            print("WARN: texture not found: " + src)
            return None
        os.makedirs(self.textures_dir, exist_ok=True)
        stem, ext = os.path.splitext(os.path.basename(src))
        if ext.lower() in self.ENGINE_FORMATS:
            name = os.path.basename(src)
            shutil.copyfile(src, os.path.join(self.textures_dir, name))
        else:
            name = stem + '.png'
            self.convert_image(src, os.path.join(self.textures_dir, name), 'PNG', is_data)
        self.exported_textures[src] = name
        return name

    def convert_image(self, src, dst, file_format, is_data):
        """Re-saves an image in another format through a throwaway datablock,
        so the user's own images keep their path and settings"""
        img = bpy.data.images.load(src, check_existing=False)
        try:
            if is_data:
                img.colorspace_settings.name = 'Non-Color'
            # Pixels load lazily, and file_format lives on the loaded pixels: set
            # before they exist it is dropped, and save() writes the source format.
            # Reading an element loads them; the bare array would not
            img.size[0]
            img.file_format = file_format
            # Not by repointing filepath, which the lazy load would then read from
            img.save(filepath=dst)
        finally:
            bpy.data.images.remove(img)

    def write_environment(self):
        """Writes the World's Environment Texture as a Radiance .hdr, the maps
        cmgen bakes from it into textures/<name>/, and the [environment] table;
        Strength and the Mapping Z rotation come along"""
        world = self.context.scene.world
        if world is None or not world.use_nodes:
            return
        nodes = world.node_tree.nodes
        env = next((n for n in nodes if n.type == 'TEX_ENVIRONMENT' and n.image), None)
        if env is None:
            print("WARN: no Environment Texture in the world, the engine will use a flat grey")
            return
        background = next((n for n in nodes if n.type == 'BACKGROUND'), None)
        mapping = next((n for n in nodes if n.type == 'MAPPING'), None)

        src = bpy.path.abspath(env.image.filepath)
        os.makedirs(self.textures_dir, exist_ok=True)
        name = os.path.splitext(os.path.basename(src))[0] + '.hdr'
        dst = os.path.join(self.textures_dir, name)
        if src.lower().endswith('.hdr'):
            shutil.copyfile(src, dst)
        else:
            self.convert_image(src, dst, 'HDR', is_data=False)
        # From the source rather than the .hdr, which cmgen reads EXR for: it names
        # the folder after the source, the same stem the engine derives from <file>
        try:
            subprocess.run([self.cmgen, *self.CMGEN_FLAGS, '--ibl-ld=' + self.textures_dir,
                            '--ibl-irradiance=' + self.textures_dir, src], check=True)
        except (OSError, subprocess.CalledProcessError) as e:
            raise RuntimeError(f"cmgen failed on {src}: {e}; set the cmgen path in the export options") from e

        environment = {"file": name}
        if background is not None:
            environment["strength"] = background.inputs['Strength'].default_value
        if mapping is not None:
            environment["rotation"] = self.degrees(mapping.inputs['Rotation'].default_value[2])
        self.data['environment'] = environment

    def write_light(self, light):
        direction = Vector((0, 0, -1))
        direction.rotate(light.rotation_euler)
        entry = {
            "name": light.name,
            "type": light.data.type.lower(),
            "position": light.location,
            "direction": direction,
            "color": light.data.color,
            "diffuse": getattr(light.data, 'diffuse_factor', 1.0),
            "intensity": light.data.energy,
        }
        # Spot cone, exported in Blender's own units so the scene round-trips:
        # the full angle in degrees and the 0..1 soft-edge fraction. toLight
        # turns them into the two cosines the shader compares against
        if light.data.type == 'SPOT':
            entry["cone"] = self.degrees(light.data.spot_size)
            entry["coneBlend"] = light.data.spot_blend
        self.data['light'].append(entry)


class OverdriveExporter(bpy.types.Operator, ExportHelper):
    """Export a blender scene into Overdrive scene format"""

    # add to menu
    bl_idname = "export_scene.ovd"
    bl_label = "Export Overdrive scene"

    filename_ext = ".toml"
    filter_glob: StringProperty(default="*.toml", options={'HIDDEN'})

    # Filament's cmgen, which bakes the environment's lighting maps; a bare name
    # is looked up on PATH, then in ~/.local/bin
    cmgen_path: StringProperty(name="cmgen", default="cmgen", subtype='FILE_PATH')

    def execute(self, context):
        ovd = OverdriveWriter(context, self.filepath, bpy.path.abspath(self.cmgen_path))
        try:
            ovd.write()
        except RuntimeError as e:
            self.report({'ERROR'}, str(e))
            return {'CANCELLED'}
        return {'FINISHED'}

def menu_func_export(self, context):
    self.layout.operator(OverdriveExporter.bl_idname, text="Overdrive (.toml)")


def register():
    bpy.utils.register_class(OverdriveExporter)
    bpy.types.TOPBAR_MT_file_export.append(menu_func_export)


def unregister():
    bpy.utils.unregister_class(OverdriveExporter)
    bpy.types.TOPBAR_MT_file_export.remove(menu_func_export)


if __name__ == "__main__":
    register()
