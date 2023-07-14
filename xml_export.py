import math
import os
import shutil
from xml.dom.minidom import Document

import bpy
import bpy_extras
from bpy.props import BoolProperty, IntProperty, StringProperty
from bpy_extras.io_utils import ExportHelper
from mathutils import Matrix, Vector

bl_info = {
    "name": "Export Overdrive scenes format",
    "author": "Zephyr",
    "version": (0, 1),
    "blender": (3, 6, 0),
    "location": "File > Export > Overdrive scene [xml]",
    "description": "Export scene using Overdrive format [xml]",
    "warning": "",
    "wiki_url": "",
    "tracker_url": "",
    "category": "Import-Export"}


class OverdriveWriter:

    def __init__(self, context, filepath):
        self.context = context
        self.filepath = filepath
        self.working_dir = os.path.dirname(self.filepath)

    def create_xml_element(self, name, attr):
        el = self.doc.createElement(name)
        for k, v in attr.items():
            el.setAttribute(k, v)
        return el

    def create_xml_entry(self, t, name, value):
        return self.create_xml_element(t, {"name": name, "value": value})

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

    def create_xml_mesh_entry(self, filename):
        meshElement = self.create_xml_element("mesh", {"type": "obj"})
        meshElement.appendChild(self.create_xml_element("string", {"name": "filename", "value": "meshes/"+filename}))
        return meshElement

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

        # 4) write the xml file
        self.doc.writexml(open(self.filepath, "w"), "", "\t", "\n")

    def write_vector(self, vec):
        return str(vec[0]) + "," + str(vec[1]) + "," + str(vec[2])
        

    def write_camera(self, cam):
        """convert the selected camera (cam) into xml format"""
        camera = self.create_xml_element("camera", {"type": "perspective"})
        position = cam.location
        front = cam.matrix_world.to_quaternion() @ Vector((0, 0, -1))
        up = cam.matrix_world.to_quaternion() @ Vector((0, 1, 0))
        yaw = cam.rotation_euler[2]
        pitch = cam.rotation_euler[1]
        fov = cam.data.angle * 180 / math.pi

        camera.appendChild(self.create_xml_entry("vector", "position", self.write_vector(position)))
        camera.appendChild(self.create_xml_entry("vector", "front", self.write_vector(front)))
        camera.appendChild(self.create_xml_entry("vector", "up", self.write_vector(up)))
        camera.appendChild(self.create_xml_entry("float", "yaw", str(yaw)))
        camera.appendChild(self.create_xml_entry("float", "pitch", str(pitch)))
        camera.appendChild(self.create_xml_entry("float", "fov", str(fov)))


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
        obj_path = os.path.join(self.working_dir, 'meshes', obj_name)
        mesh.select_set(True)
        bpy.ops.export_scene.obj(filepath=obj_path, check_existing=False,
                                    use_selection=True, use_edges=False, use_smooth_groups=False,
                                    use_materials=False, use_triangles=True, use_mesh_modifiers=True)
        mesh.select_set(False)

        # Add the corresponding entry to the xml
        mesh_element = self.create_xml_mesh_entry(obj_name)
        self.scene.appendChild(mesh_element)

        for ob in viewport_selection:
            ob.select_set(True)

    def write_light(self, light):
        light_element = self.create_xml_element("emitter", {"type": light.data.type.lower()})
        color = light.data.color
        light_element.appendChild(self.create_xml_entry("color", "color", self.write_vector(color)))
        light_element.appendChild(self.create_xml_entry("float", "intensity", str(light.data.energy)))
        self.scene.appendChild(light_element)


class OverdriveExporter(bpy.types.Operator, ExportHelper):
    """Export a blender scene into Overdrive scene format"""

    # add to menu
    bl_idname = "export_scene.ovd"
    bl_label = "Export Overdrive scene"

    filename_ext = ".xml"
    filter_glob: StringProperty(default="*.xml", options={'HIDDEN'})

    def execute(self, context):
        ovd = OverdriveWriter(context, self.filepath)
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
