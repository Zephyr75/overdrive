import math
import os
import shutil
from xml.dom.minidom import Document

import bpy
import bpy_extras
from bpy.props import BoolProperty, FloatProperty, IntProperty, StringProperty
from bpy_extras.io_utils import ExportHelper
from mathutils import Matrix, Vector

bl_info = {
    "name": "Export Overdrive scenes format",
    "author": "Zephyr",
    "version": (0, 1),
    "blender": (4, 0, 0),
    "location": "File > Export > Overdrive scene [xml]",
    "description": "Export scene using Overdrive format [xml]",
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

    def __init__(self, context, filepath, env_clamp=0.0):
        self.context = context
        self.filepath = filepath
        self.working_dir = os.path.dirname(self.filepath)
        self.textures_dir = os.path.join(self.working_dir, 'textures')
        self.env_clamp = env_clamp
        # Source path to exported name, so a texture shared by meshes is converted once
        self.exported_textures = {}

    def create_xml_element(self, name, attr):
        el = self.doc.createElement(name)
        for k, v in attr.items():
            el.setAttribute(k, v)
        return el

    # def create_xml_entry(self, t, name, value):
    #     return self.create_xml_element(t, {"name": name, "value": value})

    # def create_xml_transform(self, mat, el=None):
    #     transform = self.create_xml_element("transform", {"name": "toWorld"})
    #     if(el):
    #         transform.appendChild(el)
    #     value = ""
    #     for j in range(4):
    #         for i in range(4):
    #             value += str(mat[j][i]) + ","
    #     transform.appendChild(self.create_xml_element("matrix", {"value": value[:-1]}))
    #     return transform

    # def create_xml_mesh_entry(self, filename):
    #     meshElement = self.create_xml_element("mesh", {"type": "obj"})
    #     meshElement.appendChild(self.create_xml_element("string", {"name": "filename", "value": "meshes/"+filename}))
    #     return meshElement

    def write(self):
        """Main method to write the blender scene into xml format"""

        # create xml document
        self.doc = Document()
        self.scene = self.doc.createElement("scene")
        self.doc.appendChild(self.scene)

        # 1) export one camera
        cameras = [cam for cam in self.context.scene.objects
                   if cam.type in {'CAMERA'}]
        if(len(cameras) == 0):
            print("WARN: No camera to export")
        else:
            if(len(cameras) > 1):
                print("WARN: Does not handle multiple camera, only export the active one")
            self.scene.appendChild(self.write_camera(self.context.scene.camera))  # export the active one

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

        # 5) write the xml file
        self.doc.writexml(open(self.filepath, "w"), "", "\t", "\n")

    def write_vector(self, vec):
        return str(vec[0]) + "," + str(vec[1]) + "," + str(vec[2])

    def write_camera(self, cam):
        """convert the selected camera (cam) into xml format"""
        camera = self.create_xml_element("camera", {"name": cam.name})
        position = cam.location
        front = cam.matrix_world.to_quaternion() @ Vector((0, 0, -1))
        up = cam.matrix_world.to_quaternion() @ Vector((0, 1, 0))
        yaw = cam.rotation_euler[2] * 180 / math.pi
        pitch = cam.rotation_euler[0] * 180 / math.pi
        fov = cam.data.angle * 180 / math.pi

        type_element = self.create_xml_element("type", {})
        type_element.appendChild(self.doc.createTextNode(cam.data.type.lower()))
        camera.appendChild(type_element)

        position_element = self.create_xml_element("position", {})
        position_element.appendChild(self.doc.createTextNode(self.write_vector(position)))
        camera.appendChild(position_element)

        front_element = self.create_xml_element("front", {})
        front_element.appendChild(self.doc.createTextNode(self.write_vector(front)))
        camera.appendChild(front_element)

        up_element = self.create_xml_element("up", {})
        up_element.appendChild(self.doc.createTextNode(self.write_vector(up)))
        camera.appendChild(up_element)

        yaw_element = self.create_xml_element("yaw", {})
        yaw_element.appendChild(self.doc.createTextNode(str(yaw)))
        camera.appendChild(yaw_element)

        pitch_element = self.create_xml_element("pitch", {})
        pitch_element.appendChild(self.doc.createTextNode(str(pitch)))
        camera.appendChild(pitch_element)

        fov_element = self.create_xml_element("fov", {})
        fov_element.appendChild(self.doc.createTextNode(str(fov)))
        camera.appendChild(fov_element)

        # mat = cam.matrix_world

        # # Conversion to Y-up coordinate system
        # coord_transf = bpy_extras.io_utils.axis_conversion(
        #     from_forward='Y', from_up='Z', to_forward='-Z', to_up='Y').to_4x4()
        # mat = coord_transf @ mat
        # pos = mat.translation
        # # Nori's camera needs this these coordinates to be flipped
        # m = Matrix([[-1, 0, 0, 0], [0, 1, 0, 0], [0, 0, -1, 0], [0, 0, 0, 0]])
        # t = mat.to_3x3() @ m.to_3x3()
        # mat = Matrix([[t[0][0], t[0][1], t[0][2], pos[0]],
        #               [t[1][0], t[1][1], t[1][2], pos[1]],
        #               [t[2][0], t[2][1], t[2][2], pos[2]],
        #               [0, 0, 0, 1]])
        # value = ""
        # for j in range(4):
        #     for i in range(4):
        #         value += str(mat[j][i]) + ","

        # trans = self.create_xml_entry("matrix","toWorld", value[:-1])
        # camera.appendChild(trans)
        return camera

    def write_mesh(self, mesh):
        viewport_selection = self.context.selected_objects
        bpy.ops.object.select_all(action='DESELECT')

        obj_name = mesh.name + ".obj"
        mtl_name = mesh.name + ".mtl"
        obj_path = os.path.join(self.working_dir, 'meshes', obj_name)
        mesh.select_set(True)
        # PBR extensions write Pr/Pm and map_Pr, which the engine reads as
        # roughness and metallic; without them only Kd survives
        bpy.ops.wm.obj_export(filepath=obj_path, check_existing=False,
                              export_selected_objects=True, export_smooth_groups=False,
                              export_materials=True, export_triangulated_mesh=True,
                              export_pbr_extensions=True, apply_modifiers=True)
        mesh.select_set(False)
        self.localise_mtl(os.path.join(self.working_dir, 'meshes', mtl_name))

        # Add the corresponding entry to the xml
        mesh_element = self.create_xml_element("mesh", {"name": mesh.name})

        pos_element = self.create_xml_element("position", {})
        pos_element.appendChild(self.doc.createTextNode(self.write_vector(mesh.location)))
        mesh_element.appendChild(pos_element)

        rot_element = self.create_xml_element("rotation", {})
        rot_element.appendChild(self.doc.createTextNode(self.write_vector(mesh.rotation_euler)))
        mesh_element.appendChild(rot_element)

        obj_element = self.create_xml_element("obj", {})
        obj_element.appendChild(self.doc.createTextNode(obj_name))
        mesh_element.appendChild(obj_element)

        mtl_element = self.create_xml_element("mtl", {})
        mtl_element.appendChild(self.doc.createTextNode(mtl_name))
        mesh_element.appendChild(mtl_element)

        # Blender's own "Shadow" ray visibility. Written only when it is off, the
        # engine defaulting to true, so existing scenes keep meaning what they did
        if not mesh.visible_shadow:
            casts_element = self.create_xml_element("castsShadow", {})
            casts_element.appendChild(self.doc.createTextNode("false"))
            mesh_element.appendChild(casts_element)

        self.scene.appendChild(mesh_element)


        for ob in viewport_selection:
            ob.select_set(True)

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
            img.file_format = file_format
            img.filepath_raw = dst
            img.save()
        finally:
            bpy.data.images.remove(img)

    def write_environment(self):
        """Writes the World's Environment Texture as a Radiance .hdr and an
        <environment> element; Strength and the Mapping Z rotation come along"""
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

        env_element = self.create_xml_element("environment", {})
        values = [("file", name)]
        if background is not None:
            values.append(("strength", str(background.inputs['Strength'].default_value)))
        if mapping is not None:
            values.append(("rotation", str(mapping.inputs['Rotation'].default_value[2])))
        if self.env_clamp > 0:
            values.append(("clamp", str(self.env_clamp)))
        for tag, value in values:
            element = self.create_xml_element(tag, {})
            element.appendChild(self.doc.createTextNode(value))
            env_element.appendChild(element)
        self.scene.appendChild(env_element)

    def write_light(self, light):
        light_element = self.create_xml_element("light", {"name": light.name})
        pos = light.location
        dir = light.rotation_euler
        vec = Vector((0, 0, -1))
        vec.rotate(light.rotation_euler)
        dir = vec

        color = light.data.color
        diffuse = getattr(light.data, 'diffuse_factor', 1.0)
        intensity = light.data.energy

        type_element = self.create_xml_element("type", {})
        type_element.appendChild(self.doc.createTextNode(light.data.type.lower()))
        light_element.appendChild(type_element)

        pos_element = self.create_xml_element("position", {})
        pos_element.appendChild(self.doc.createTextNode(self.write_vector(pos)))
        light_element.appendChild(pos_element)

        dir_element = self.create_xml_element("direction", {})
        dir_element.appendChild(self.doc.createTextNode(self.write_vector(dir)))
        light_element.appendChild(dir_element)

        ambient_element = self.create_xml_element("color", {})
        ambient_element.appendChild(self.doc.createTextNode(self.write_vector(color)))
        light_element.appendChild(ambient_element)

        diffuse_element = self.create_xml_element("diffuse", {})
        diffuse_element.appendChild(self.doc.createTextNode(str(diffuse)))
        light_element.appendChild(diffuse_element)

        # Spot cone, exported in Blender's own units so the scene round-trips:
        # the full angle in degrees and the 0..1 soft-edge fraction. toLight
        # turns them into the two cosines the shader compares against
        if light.data.type == 'SPOT':
            cone_element = self.create_xml_element("cone", {})
            cone_element.appendChild(self.doc.createTextNode(
                str(math.degrees(light.data.spot_size))))
            light_element.appendChild(cone_element)

            blend_element = self.create_xml_element("coneBlend", {})
            blend_element.appendChild(self.doc.createTextNode(
                str(light.data.spot_blend)))
            light_element.appendChild(blend_element)

        intensity_element = self.create_xml_element("intensity", {})
        intensity_element.appendChild(self.doc.createTextNode(str(intensity)))
        light_element.appendChild(intensity_element)

        self.scene.appendChild(light_element)


class OverdriveExporter(bpy.types.Operator, ExportHelper):
    """Export a blender scene into Overdrive scene format"""

    # add to menu
    bl_idname = "export_scene.ovd"
    bl_label = "Export Overdrive scene"

    filename_ext = ".xml"
    filter_glob: StringProperty(default="*.xml", options={'HIDDEN'})

    # Caps the environment's radiance in the engine, 0 for none: set it below the
    # sun when a Sun lamp stands in for it, or the sun is counted twice
    env_clamp: FloatProperty(name="Environment clamp", default=0.0, min=0.0)

    def execute(self, context):
        ovd = OverdriveWriter(context, self.filepath, self.env_clamp)
        ovd.write()
        return {'FINISHED'}

def menu_func_export(self, context):
    self.layout.operator(OverdriveExporter.bl_idname, text="Export Overdrive scene...")


def register():
    bpy.utils.register_class(OverdriveExporter)
    bpy.types.TOPBAR_MT_file_export.append(menu_func_export)


def unregister():
    bpy.utils.unregister_class(OverdriveExporter)
    bpy.types.TOPBAR_MT_file_export.remove(menu_func_export)


if __name__ == "__main__":
    register()
