import type { PagesCopy } from './en.ts'

export const frPages: PagesCopy = {
  "nav": {
    "tutorial": "Tutoriel",
    "packages": "Paquets",
    "examples": "Exemples",
    "getStarted": "Commencer",
    "search": "Rechercher"
  },
  "home": {
    "eyebrow": "Début du projet : les API évolueront",
    "title": [
      "Écrivez vos jeux en Go.",
      "Un seul code, six plateformes."
    ],
    "lead": "game est la couche jeu de NSGo : boucle à pas fixe, actions d’entrée, sprites, tilemaps, scènes et son. Le jeu se dessine dans un Canvas ; son HUD et ses menus restent des vues NSGo natives.",
    "start": "Commencer",
    "playground": "Essayer le Playground",
    "live": "LIVE · WEB BUILD",
    "readouts": [
      "Boucle à pas fixe",
      "Résolution virtuelle mise à l’échelle",
      "Une image de 1000 sprites"
    ],
    "platforms": "Un seul code Go, compilé pour →",
    "consoles": "Les consoles (Switch, PlayStation, Xbox) sont hors périmètre : leurs SDK sont sous NDA et exigent des chaînes d’outils C, alors que ce projet est 100 % Go.",
    "howTitle": "Le jeu est dessiné. L’interface est native.",
    "how": [
      {
        "title": "Dessinez le jeu dans une vue",
        "body": "Le jeu se dessine dans une vue NSGo : un Canvas, un tampon de pixels ou un shader. La boucle, les lots de sprites et les tilemaps s’organisent autour."
      },
      {
        "title": "Des vues natives pour le HUD et les menus",
        "body": "Scores, menus de pause et réglages sont des vues NSGo ordinaires posées sur le jeu, avec texte, accessibilité et méthodes de saisie natifs."
      },
      {
        "title": "Ne liez que ce que vous utilisez",
        "body": "Seule l’API publique de NSGo est utilisée : une app qui ne l’importe pas n’en lie rien, et chaque paquet s’importe séparément."
      }
    ],
    "hudNote": "HUD · vues NSGo natives",
    "canvasNote": "Jeu · core.Canvas",
    "stagesTitle": "Deux étapes, un socle commun.",
    "stagesLead": "La boucle, les actions d’entrée, le son et le changement de scène sont partagés : la 3D ajoute du dessin sans rien remplacer.",
    "stages": [
      {
        "status": "Disponible",
        "body": "Boucle à pas fixe, actions d’entrée, sprites, tilemaps, scènes et son, rendus à 60 fps ou à la fréquence de l’écran."
      },
      {
        "status": "En cours",
        "body": "Maillages, caméras, lumières et modèles glTF dessinés avec profondeur sur la vue GPU que NSGo ajoute pour cela, avec import depuis Blender."
      }
    ],
    "codeTitle": "Un composant, un jeu.",
    "codeLead": "Un jeu est un composant NSGo : il lie ses actions une fois, lance un game.Loop sur sa portée, fait avancer les actions à chaque pas fixe et redessine un Canvas dans Draw. Voici le squelette de Cave Run, réduit à un sprite.",
    "codePoints": [
      "Mises à jour à pas fixe, images interpolées",
      "Clavier et manette liés à la même action",
      "Rechargement à chaud : go tool nsgo dev"
    ],
    "packagesTitle": "{n} paquets. N’importez que l’essentiel.",
    "packagesLead": "Des sprites aux serveurs dédiés, chaque paquet a son guide et est marqué stable ou expérimental.",
    "packagesLink": "Parcourir tous les paquets",
    "examplesTitle": "Jeux d’exemple",
    "examplesLead": "Les exemples tournent sur ordinateur, mobile et web, et leur code est dans le dépôt.",
    "allExamples": "Voir tous les exemples",
    "fieldmedic": "Un petit jeu livré avec son propre éditeur de scènes et un plugin d’éditeur.",
    "starCatcher": "Le tutoriel : neuf chapitres, du projet vide au jeu publié.",
    "startTitle": "Trois commandes pour votre premier jeu.",
    "steps": [
      "Créez un projet",
      "Lancez-le ici, rechargement à chaud",
      "Passez à une autre cible"
    ],
    "requirements": "Nécessite Go 1.27+ et NSGo v0.3.4+. Les builds iOS demandent macOS et Xcode.",
    "migrate": "Vous venez d’un autre moteur :",
    "finalTitle": "Encore jeune, mais déjà de quoi publier un jeu complet.",
    "finalLead": "Rien n’est encore publié. La politique de stabilité de l’API indique quels paquets sont stables et lesquels peuvent encore changer.",
    "finalStart": "Lire le guide de démarrage",
    "finalRoadmap": "Voir la feuille de route"
  },
  "categories": [
    "Cœur",
    "Gameplay",
    "Graphismes et UI",
    "Outils",
    "En ligne",
    "3D"
  ],
  "genres": {
    "caverun": "Plateforme",
    "cryptquest": "Aventure en donjon",
    "starburst": "Shooter d’arène",
    "fountain": "Test de particules",
    "slidegem": "Puzzle match-3",
    "lantern3d": "Scènes 2D ↔ 3D"
  },
  "blurbs": {
    "caverun": "Un jeu de plateforme à défilement horizontal : un explorateur traverse trois cavernes, ramasse des gemmes, saute par-dessus slimes et lave et atteint le portail de sortie. Il se dessine dans un Canvas ; ses écrans de titre, de pause et de fin sont des vues natives.",
    "cryptquest": "Une aventure en donjon à tuiles vue de dessus, sur les six cibles.",
    "starburst": "Un shooter d’arène vu de dessus à deux sticks, avec particules, physique et entrées.",
    "fountain": "Test de charge : quatre fontaines maintiennent 5000 particules en vie.",
    "slidegem": "Un match-3 coulissant 6×6 pensé pour le tactile, sans aucun fichier de ressources externe.",
    "lantern3d": "Passe en fondu d’un sanctuaire de lanternes en 3D à une carte de la cour en 2D : un diorama glTF, un éclairage PBR et un HUD natif, les deux scènes partageant entrées et son."
  },
  "packages": {
    "title": "{n} paquets. N’importez que l’essentiel.",
    "lead": "Des sprites aux serveurs dédiés, chaque paquet a son guide et est marqué stable ou expérimental.",
    "search": "Rechercher un paquet",
    "all": "Tous",
    "guide": "Guide",
    "noMatch": "Aucun paquet ne correspond"
  },
  "examples": {
    "title": "Jeux d’exemple",
    "lead": "Les exemples tournent sur ordinateur, mobile et web, et leur code est dans le dépôt.",
    "used": "Paquets utilisés",
    "run": "Lancer",
    "play": "Jouer dans le navigateur",
    "source": "Voir le code",
    "lanternCaption": "Sur l’image, la carte 2D de la cour. Jouez pour voir le sanctuaire 3D."
  },
  "tutorial": {
    "eyebrow": "Neuf chapitres",
    "start": "Commencer au chapitre 1",
    "source": "Voir le code",
    "run": "Lancer les chapitres depuis le dépôt"
  },
  "footer": {
    "tagline": "La couche jeu de NSGo, écrite en Go. La 2D aujourd’hui, la 3D bientôt.",
    "columns": [
      "DOCS",
      "PROJECT",
      "COMMUNITY"
    ],
    "gettingStarted": "Démarrage",
    "tutorial": "Tutoriel : Star Catcher",
    "packages": "Paquets",
    "stability": "Stabilité de l’API",
    "playground": "Playground",
    "examples": "Exemples",
    "roadmap": "Feuille de route",
    "changelog": "Journal des modifications",
    "github": "GitHub",
    "nsgo": "NSGo",
    "contributing": "Contribuer",
    "feedback": "Retours",
    "license": "Licence Apache-2.0 · © 2026 NSGo",
    "llms": "Docs pour LLM (llms.txt)",
    "llmsFull": "Docs complets pour LLM (llms-full.txt)",
    "showcase": "Vitrine"
  },
  "docsNotice": "Cette page est disponible en anglais."
}
